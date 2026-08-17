package auth

import (
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"

	"github.com/cyberrange-os/api/internal/audit"
	"github.com/cyberrange-os/api/internal/config"
	"github.com/cyberrange-os/api/internal/httpx"
)

type Handler struct {
	cfg    *config.Config
	store  *Store
	issuer *TokenIssuer
	audit  *audit.Logger
	oidc   *OIDCProvider
	log    zerolog.Logger
}

func NewHandler(cfg *config.Config, store *Store, issuer *TokenIssuer, auditor *audit.Logger, log zerolog.Logger) *Handler {
	h := &Handler{cfg: cfg, store: store, issuer: issuer, audit: auditor, log: log}
	if cfg.OIDCIssuer != "" && cfg.OIDCClientID != "" {
		h.oidc = NewOIDCProvider(cfg, log)
	}
	return h
}

func (h *Handler) Register(r fiber.Router) {
	g := r.Group("/auth")
	g.Post("/login", h.login)
	g.Post("/refresh", h.refresh)
	g.Post("/logout", h.logout)
	g.Get("/providers", h.providers)
	g.Get("/sso/start", h.ssoStart)
	g.Get("/sso/callback", h.ssoCallback)
}

type loginRequest struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

type sessionResponse struct {
	AccessToken string    `json:"access_token"`
	ExpiresAt   time.Time `json:"expires_at"`
	User        *User     `json:"user"`
}

const refreshCookie = "cr_refresh"

func (h *Handler) login(c *fiber.Ctx) error {
	if !h.cfg.AllowLocalPassword {
		return httpx.Forbidden("local password login is disabled; use institution SSO")
	}
	body, err := httpx.Bind[loginRequest](c)
	if err != nil {
		return err
	}
	if strings.TrimSpace(body.Login) == "" || body.Password == "" {
		return httpx.BadRequest("login and password are required")
	}

	user, hash, err := h.store.ByLogin(c.Context(), body.Login)
	if err != nil || hash == "" || !CheckPassword(hash, body.Password) {
		h.audit.FromCtx(c, "auth.login.failed", "user", strings.ToLower(body.Login), audit.SevWarning,
			map[string]any{"reason": "invalid_credentials"})
		return httpx.Unauthorized("invalid credentials")
	}
	if !user.IsActive {
		return httpx.Forbidden("account is deactivated")
	}
	return h.issueSession(c, user, "local")
}

func (h *Handler) issueSession(c *fiber.Ctx, user *User, via string) error {
	roll := ""
	if user.RollNo != nil {
		roll = *user.RollNo
	}
	access, exp, err := h.issuer.IssueAccess(user.ID, user.Role, user.Name, roll)
	if err != nil {
		return httpx.Internal("failed to issue access token")
	}
	plain, hash, err := NewRefreshToken()
	if err != nil {
		return httpx.Internal("failed to issue refresh token")
	}
	if _, err := h.store.StoreRefresh(c.Context(), user.ID, hash, h.issuer.RefreshTTL(), c.Get("User-Agent"), c.IP()); err != nil {
		return httpx.Internal("failed to persist refresh token")
	}
	h.setRefreshCookie(c, plain, h.issuer.RefreshTTL())
	h.store.TouchLogin(c.Context(), user.ID)

	batches, _ := h.store.BatchesFor(c.Context(), user.ID, user.Role)
	user.Batches = batches

	h.audit.Write(c.Context(), audit.Entry{
		ActorID:    &user.ID,
		ActorRole:  string(user.Role),
		Action:     "auth.login",
		TargetType: "user",
		TargetID:   user.ID.String(),
		Severity:   audit.SevInfo,
		IP:         c.IP(),
		Metadata:   map[string]any{"via": via},
	})
	return httpx.OK(c, sessionResponse{AccessToken: access, ExpiresAt: exp, User: user})
}

func (h *Handler) setRefreshCookie(c *fiber.Ctx, value string, ttl time.Duration) {
	c.Cookie(&fiber.Cookie{
		Name:     refreshCookie,
		Value:    value,
		Path:     "/",
		Domain:   h.cfg.CookieDomain,
		Expires:  time.Now().Add(ttl),
		HTTPOnly: true,
		Secure:   h.cfg.CookieSecure,
		SameSite: "Lax",
	})
}

func (h *Handler) refresh(c *fiber.Ctx) error {
	plain := c.Cookies(refreshCookie)
	if plain == "" {
		// Non-browser clients (CLI tooling) may post the token explicitly.
		body, _ := httpx.Bind[struct {
			RefreshToken string `json:"refresh_token"`
		}](c)
		plain = body.RefreshToken
	}
	if plain == "" {
		return httpx.Unauthorized("missing refresh token")
	}
	rec, err := h.store.FindRefresh(c.Context(), HashToken(plain))
	if err != nil {
		return httpx.Unauthorized("invalid refresh token")
	}
	if rec.RevokedAt != nil {
		// Token reuse after rotation: treat as compromise, kill the family.
		_ = h.store.RevokeAllForUser(c.Context(), rec.UserID)
		h.audit.Write(c.Context(), audit.Entry{
			ActorID: &rec.UserID, Action: "auth.refresh.reuse_detected", TargetType: "user",
			TargetID: rec.UserID.String(), Severity: audit.SevCritical, IP: c.IP(),
		})
		return httpx.Unauthorized("refresh token already used; please sign in again")
	}
	if time.Now().After(rec.ExpiresAt) {
		return httpx.Unauthorized("refresh token expired")
	}
	user, err := h.store.ByID(c.Context(), rec.UserID)
	if err != nil {
		return httpx.Unauthorized("user no longer exists")
	}
	if !user.IsActive {
		return httpx.Forbidden("account is deactivated")
	}

	newPlain, newHash, err := NewRefreshToken()
	if err != nil {
		return httpx.Internal("failed to rotate refresh token")
	}
	newID, err := h.store.StoreRefresh(c.Context(), user.ID, newHash, h.issuer.RefreshTTL(), c.Get("User-Agent"), c.IP())
	if err != nil {
		return httpx.Internal("failed to persist refresh token")
	}
	if err := h.store.RotateRefresh(c.Context(), rec.ID, newID); err != nil {
		return httpx.Internal("failed to rotate refresh token")
	}
	h.setRefreshCookie(c, newPlain, h.issuer.RefreshTTL())

	roll := ""
	if user.RollNo != nil {
		roll = *user.RollNo
	}
	access, exp, err := h.issuer.IssueAccess(user.ID, user.Role, user.Name, roll)
	if err != nil {
		return httpx.Internal("failed to issue access token")
	}
	batches, _ := h.store.BatchesFor(c.Context(), user.ID, user.Role)
	user.Batches = batches
	return httpx.OK(c, sessionResponse{AccessToken: access, ExpiresAt: exp, User: user})
}

func (h *Handler) logout(c *fiber.Ctx) error {
	if plain := c.Cookies(refreshCookie); plain != "" {
		_ = h.store.RevokeRefresh(c.Context(), HashToken(plain))
	}
	h.setRefreshCookie(c, "", -time.Hour)
	if id, ok := Current(c); ok {
		h.audit.Write(c.Context(), audit.Entry{
			ActorID: &id.UserID, ActorRole: string(id.Role), Action: "auth.logout",
			TargetType: "user", TargetID: id.UserID.String(), IP: c.IP(),
		})
	}
	return httpx.NoContent(c)
}

func (h *Handler) providers(c *fiber.Ctx) error {
	return httpx.OK(c, fiber.Map{
		"local": h.cfg.AllowLocalPassword,
		"sso":   h.oidc != nil,
		"sso_label": func() string {
			if h.oidc != nil {
				return "Institution SSO"
			}
			return ""
		}(),
		"saml_metadata": h.cfg.SAMLMetadataURL,
	})
}

// Me returns the caller's profile with batch enrollments.
func (h *Handler) Me(c *fiber.Ctx) error {
	id, err := MustCurrent(c)
	if err != nil {
		return err
	}
	user, err := h.store.ByID(c.Context(), id.UserID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return httpx.NotFound("user not found")
		}
		return httpx.Internal("failed to load user")
	}
	batches, err := h.store.BatchesFor(c.Context(), user.ID, user.Role)
	if err != nil {
		return httpx.Internal("failed to load batches")
	}
	user.Batches = batches
	return httpx.OK(c, user)
}

func (h *Handler) Store() *Store { return h.store }
