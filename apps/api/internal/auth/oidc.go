package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"

	"github.com/cyberrange-os/api/internal/audit"
	"github.com/cyberrange-os/api/internal/config"
	"github.com/cyberrange-os/api/internal/httpx"
)

// OIDCProvider implements the authorization-code flow against the college
// identity provider using OIDC discovery. Identity is confirmed through the
// provider's userinfo endpoint with the freshly issued access token, so no
// local JWKS cache is required.
type OIDCProvider struct {
	cfg  *config.Config
	log  zerolog.Logger
	http *http.Client

	mu       sync.RWMutex
	meta     *oidcMetadata
	fetched  time.Time
	stateTTL map[string]time.Time
}

type oidcMetadata struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
	EndSessionEndpoint    string `json:"end_session_endpoint"`
}

func NewOIDCProvider(cfg *config.Config, log zerolog.Logger) *OIDCProvider {
	return &OIDCProvider{
		cfg:      cfg,
		log:      log,
		http:     &http.Client{Timeout: 15 * time.Second},
		stateTTL: map[string]time.Time{},
	}
}

func (p *OIDCProvider) metadata(ctx context.Context) (*oidcMetadata, error) {
	p.mu.RLock()
	if p.meta != nil && time.Since(p.fetched) < time.Hour {
		m := p.meta
		p.mu.RUnlock()
		return m, nil
	}
	p.mu.RUnlock()

	discovery := strings.TrimRight(p.cfg.OIDCIssuer, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discovery, nil)
	if err != nil {
		return nil, err
	}
	res, err := p.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oidc discovery returned %d", res.StatusCode)
	}
	var m oidcMetadata
	if err := json.NewDecoder(res.Body).Decode(&m); err != nil {
		return nil, fmt.Errorf("decode oidc discovery: %w", err)
	}
	p.mu.Lock()
	p.meta, p.fetched = &m, time.Now()
	p.mu.Unlock()
	return &m, nil
}

func (p *OIDCProvider) newState() string {
	s := RandomSecret(16)
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	for k, exp := range p.stateTTL {
		if now.After(exp) {
			delete(p.stateTTL, k)
		}
	}
	p.stateTTL[s] = now.Add(10 * time.Minute)
	return s
}

func (p *OIDCProvider) consumeState(s string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	exp, ok := p.stateTTL[s]
	if !ok {
		return false
	}
	delete(p.stateTTL, s)
	return time.Now().Before(exp)
}

func (h *Handler) ssoStart(c *fiber.Ctx) error {
	if h.oidc == nil {
		return httpx.NotFound("institution SSO is not configured")
	}
	meta, err := h.oidc.metadata(c.Context())
	if err != nil {
		return httpx.Unavailable("identity provider unreachable: " + err.Error())
	}
	state := h.oidc.newState()
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", h.cfg.OIDCClientID)
	q.Set("redirect_uri", h.cfg.OIDCRedirectURL)
	q.Set("scope", "openid profile email")
	q.Set("state", state)
	return c.Redirect(meta.AuthorizationEndpoint+"?"+q.Encode(), fiber.StatusFound)
}

type oidcTokenResponse struct {
	AccessToken string `json:"access_token"`
	IDToken     string `json:"id_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

type oidcUserinfo struct {
	Sub               string `json:"sub"`
	Email             string `json:"email"`
	Name              string `json:"name"`
	PreferredUsername string `json:"preferred_username"`
	// Institution IdPs commonly expose a role/affiliation claim; the exact
	// claim name is mapped here and can be adjusted per deployment.
	Role         string `json:"role"`
	Affiliation  string `json:"eduPersonAffiliation"`
	EmployeeType string `json:"employeeType"`
	RollNumber   string `json:"rollNumber"`
}

func (h *Handler) ssoCallback(c *fiber.Ctx) error {
	if h.oidc == nil {
		return httpx.NotFound("institution SSO is not configured")
	}
	code := c.Query("code")
	state := c.Query("state")
	if code == "" || state == "" {
		return httpx.BadRequest("missing code or state")
	}
	if !h.oidc.consumeState(state) {
		return httpx.BadRequest("invalid or expired state")
	}
	meta, err := h.oidc.metadata(c.Context())
	if err != nil {
		return httpx.Unavailable("identity provider unreachable")
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", h.cfg.OIDCRedirectURL)
	form.Set("client_id", h.cfg.OIDCClientID)
	form.Set("client_secret", h.cfg.OIDCClientSecret)

	req, err := http.NewRequestWithContext(c.Context(), http.MethodPost, meta.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return httpx.Internal("failed to build token request")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := h.oidc.http.Do(req)
	if err != nil {
		return httpx.Unavailable("token exchange failed: " + err.Error())
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return httpx.Unauthorized("token exchange rejected by identity provider")
	}
	var tok oidcTokenResponse
	if err := json.NewDecoder(res.Body).Decode(&tok); err != nil {
		return httpx.Internal("invalid token response")
	}

	uiReq, err := http.NewRequestWithContext(c.Context(), http.MethodGet, meta.UserinfoEndpoint, nil)
	if err != nil {
		return httpx.Internal("failed to build userinfo request")
	}
	uiReq.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	uiRes, err := h.oidc.http.Do(uiReq)
	if err != nil {
		return httpx.Unavailable("userinfo request failed")
	}
	defer uiRes.Body.Close()
	if uiRes.StatusCode != http.StatusOK {
		return httpx.Unauthorized("userinfo rejected by identity provider")
	}
	var info oidcUserinfo
	if err := json.NewDecoder(uiRes.Body).Decode(&info); err != nil {
		return httpx.Internal("invalid userinfo response")
	}
	if info.Email == "" {
		return httpx.BadRequest("identity provider did not return an email claim")
	}

	user, err := h.store.UpsertExternal(c.Context(), CreateUserInput{
		Role:         mapIdPRole(info),
		Name:         firstNonEmpty(info.Name, info.PreferredUsername, info.Email),
		Email:        info.Email,
		RollNo:       info.RollNumber,
		AuthProvider: "oidc",
		ExternalID:   info.Sub,
	})
	if err != nil {
		h.log.Error().Err(err).Msg("sso user provisioning failed")
		return httpx.Internal("failed to provision SSO user")
	}
	if !user.IsActive {
		return httpx.Forbidden("account is deactivated")
	}

	h.audit.Write(c.Context(), audit.Entry{
		ActorID: &user.ID, ActorRole: string(user.Role), Action: "auth.sso.login",
		TargetType: "user", TargetID: user.ID.String(), IP: c.IP(),
		Metadata: map[string]any{"issuer": meta.Issuer},
	})

	// Hand the browser back to the web app with a one-time refresh cookie;
	// the SPA immediately calls /api/auth/refresh to obtain an access token.
	plain, hash, err := NewRefreshToken()
	if err != nil {
		return httpx.Internal("failed to issue refresh token")
	}
	if _, err := h.store.StoreRefresh(c.Context(), user.ID, hash, h.issuer.RefreshTTL(), c.Get("User-Agent"), c.IP()); err != nil {
		return httpx.Internal("failed to persist refresh token")
	}
	h.setRefreshCookie(c, plain, h.issuer.RefreshTTL())
	h.store.TouchLogin(c.Context(), user.ID)
	return c.Redirect(strings.TrimRight(h.cfg.PublicURL, "/")+"/auth/sso-complete", fiber.StatusFound)
}

func mapIdPRole(info oidcUserinfo) Role {
	candidates := []string{info.Role, info.Affiliation, info.EmployeeType}
	for _, raw := range candidates {
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case "faculty", "teacher", "staff", "instructor":
			return RoleFaculty
		case "admin", "labadmin", "sysadmin":
			return RoleAdmin
		case "auditor", "hod", "reviewer":
			return RoleAuditor
		case "student", "member":
			return RoleStudent
		}
	}
	return RoleStudent
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
