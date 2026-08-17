package server

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/cyberrange-os/api/internal/audit"
	"github.com/cyberrange-os/api/internal/auth"
	"github.com/cyberrange-os/api/internal/httpx"
	"github.com/cyberrange-os/api/internal/llm"
	"github.com/cyberrange-os/api/internal/store"
)

type adminHandler struct{ d *Deps }

func newAdminHandler(d *Deps) *adminHandler { return &adminHandler{d: d} }

func (h *adminHandler) register(r fiber.Router) {
	// Users: admins manage everyone; faculty may list students for enrollment.
	users := r.Group("/users")
	users.Get("/", auth.RequireRole(auth.RoleAdmin, auth.RoleFaculty), h.listUsers)
	users.Post("/", auth.RequireRole(auth.RoleAdmin), h.createUser)
	users.Put("/:id/role", auth.RequireRole(auth.RoleAdmin), h.setRole)
	users.Put("/:id/active", auth.RequireRole(auth.RoleAdmin), h.setActive)
	users.Put("/:id/password", auth.RequireRole(auth.RoleAdmin), h.setPassword)

	g := r.Group("/admin", auth.RequireRole(auth.RoleAdmin))
	// LLM registry
	g.Get("/llm-models", h.listModels)
	g.Post("/llm-models", h.createModel)
	g.Put("/llm-models/:id", h.updateModel)
	g.Delete("/llm-models/:id", h.deleteModel)
	g.Get("/llm-models/discover", h.discoverModels)
	// Prompts
	g.Get("/llm-prompts", h.listPrompts)
	g.Post("/llm-prompts", h.createPrompt)
	g.Post("/llm-prompts/:id/activate", h.activatePrompt)
	// LLM egress + health
	g.Get("/llm-egress", h.egress)
	g.Post("/llm-egress/verify", h.verifyEgress)
	// Range targets
	g.Post("/range-targets", h.createTarget)
	g.Delete("/range-targets/:id", h.deleteTarget)
	g.Put("/range-targets/:id/active", h.setTargetActive)
	// Audit + health
	g.Get("/audit-log", h.auditLog)
	g.Get("/health", h.systemHealth)
	g.Get("/settings/:key", h.getSetting)
	g.Put("/settings/:key", h.setSetting)

	// Range targets are readable by faculty/admin (for exercise authoring).
	r.Get("/range-targets", auth.RequireRole(auth.RoleAdmin, auth.RoleFaculty, auth.RoleStudent), h.listTargets)
}

// ------------------------------------------------------------------ users

func (h *adminHandler) listUsers(c *fiber.Ctx) error {
	page := httpx.ParsePage(c, 50, 200)
	users, total, err := h.d.authStore.List(c.Context(), c.Query("role"), c.Query("search"), page.Limit, page.Offset)
	if err != nil {
		return httpx.Internal("failed to list users")
	}
	return httpx.OK(c, httpx.ListResponse[auth.User]{Items: users, Total: total})
}

func (h *adminHandler) createUser(c *fiber.Ctx) error {
	body, err := httpx.Bind[struct {
		Role     string `json:"role"`
		Name     string `json:"name"`
		Email    string `json:"email"`
		RollNo   string `json:"roll_no"`
		Password string `json:"password"`
	}](c)
	if err != nil {
		return err
	}
	if !auth.ValidRole(body.Role) {
		return httpx.BadRequest("invalid role")
	}
	if body.Name == "" || body.Email == "" {
		return httpx.BadRequest("name and email are required")
	}
	u, err := h.d.authStore.Create(c.Context(), auth.CreateUserInput{
		Role: auth.Role(body.Role), Name: body.Name, Email: body.Email, RollNo: body.RollNo, Password: body.Password,
	})
	if err != nil {
		return httpx.Conflict("could not create user: " + err.Error())
	}
	h.d.Audit.FromCtx(c, "user.create", "user", u.ID.String(), audit.SevNotice,
		map[string]any{"role": body.Role, "email": body.Email})
	return httpx.Created(c, u)
}

func (h *adminHandler) setRole(c *fiber.Ctx) error {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		return err
	}
	body, err := httpx.Bind[struct {
		Role string `json:"role"`
	}](c)
	if err != nil {
		return err
	}
	if !auth.ValidRole(body.Role) {
		return httpx.BadRequest("invalid role")
	}
	if err := h.d.authStore.SetRole(c.Context(), id, auth.Role(body.Role)); err != nil {
		return httpx.Internal("failed to set role")
	}
	_ = h.d.authStore.RevokeAllForUser(c.Context(), id) // force re-auth with new role
	h.d.Audit.FromCtx(c, "rbac.role.change", "user", id.String(), audit.SevCritical,
		map[string]any{"new_role": body.Role})
	return httpx.NoContent(c)
}

func (h *adminHandler) setActive(c *fiber.Ctx) error {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		return err
	}
	body, _ := httpx.Bind[struct {
		Active bool `json:"active"`
	}](c)
	if err := h.d.authStore.SetActive(c.Context(), id, body.Active); err != nil {
		return httpx.Internal("failed to set active state")
	}
	if !body.Active {
		_ = h.d.authStore.RevokeAllForUser(c.Context(), id)
	}
	h.d.Audit.FromCtx(c, "user.set_active", "user", id.String(), audit.SevWarning,
		map[string]any{"active": body.Active})
	return httpx.NoContent(c)
}

func (h *adminHandler) setPassword(c *fiber.Ctx) error {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		return err
	}
	body, err := httpx.Bind[struct {
		Password string `json:"password"`
	}](c)
	if err != nil {
		return err
	}
	if err := h.d.authStore.SetPassword(c.Context(), id, body.Password); err != nil {
		return httpx.BadRequest(err.Error())
	}
	h.d.Audit.FromCtx(c, "user.set_password", "user", id.String(), audit.SevWarning, nil)
	return httpx.NoContent(c)
}

// ------------------------------------------------------------------ llm models

func (h *adminHandler) listModels(c *fiber.Ctx) error {
	models, err := h.d.store.ListLLMModels(c.Context())
	if err != nil {
		return httpx.Internal("failed to list models")
	}
	return httpx.OK(c, httpx.ListResponse[store.LLMModel]{Items: models, Total: len(models)})
}

func (h *adminHandler) createModel(c *fiber.Ctx) error {
	cur, _ := auth.MustCurrent(c)
	body, err := httpx.Bind[struct {
		Name          string   `json:"name"`
		Endpoint      string   `json:"endpoint"`
		Runtime       string   `json:"runtime"`
		ContextWindow int      `json:"context_window"`
		Modules       []string `json:"modules"`
		IsDefault     bool     `json:"is_default"`
		Notes         string   `json:"notes"`
	}](c)
	if err != nil {
		return err
	}
	if body.Name == "" || body.Endpoint == "" {
		return httpx.BadRequest("name and endpoint are required")
	}
	// Enforce local-only endpoints in the registry too.
	if _, gerr := llm.AssertLocalEndpoint(body.Endpoint, h.d.Cfg.LLMAllowPublicIP); gerr != nil {
		return httpx.BadRequest(gerr.Error())
	}
	if body.Runtime == "" {
		body.Runtime = "ollama"
	}
	if body.ContextWindow == 0 {
		body.ContextWindow = 8192
	}
	m, err := h.d.store.CreateLLMModel(c.Context(), store.LLMModel{
		Name: body.Name, Endpoint: body.Endpoint, Runtime: body.Runtime,
		ContextWindow: body.ContextWindow, Modules: body.Modules, IsDefault: body.IsDefault,
		Notes: body.Notes, CreatedBy: &cur.UserID,
	})
	if err != nil {
		return httpx.Conflict("could not register model: " + err.Error())
	}
	h.d.Audit.FromCtx(c, "llm.model.register", "llm_model", m.ID.String(), audit.SevNotice,
		map[string]any{"name": m.Name, "endpoint": m.Endpoint})
	return httpx.Created(c, m)
}

func (h *adminHandler) updateModel(c *fiber.Ctx) error {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		return err
	}
	body, err := httpx.Bind[struct {
		Modules   []string `json:"modules"`
		IsDefault bool     `json:"is_default"`
		IsActive  bool     `json:"is_active"`
		Notes     string   `json:"notes"`
	}](c)
	if err != nil {
		return err
	}
	if err := h.d.store.UpdateLLMModel(c.Context(), id, body.Modules, body.IsDefault, body.IsActive, body.Notes); err != nil {
		return httpx.Internal("failed to update model")
	}
	h.d.Audit.FromCtx(c, "llm.model.update", "llm_model", id.String(), audit.SevNotice, nil)
	return httpx.NoContent(c)
}

func (h *adminHandler) deleteModel(c *fiber.Ctx) error {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		return err
	}
	if err := h.d.store.DeleteLLMModel(c.Context(), id); err != nil {
		return httpx.Internal("failed to delete model")
	}
	h.d.Audit.FromCtx(c, "llm.model.delete", "llm_model", id.String(), audit.SevWarning, nil)
	return httpx.NoContent(c)
}

func (h *adminHandler) discoverModels(c *fiber.Ctx) error {
	endpoint := c.Query("endpoint")
	runtime := llm.Runtime(c.Query("runtime", "ollama"))
	models, err := h.d.Gateway.ListRemoteModels(c.Context(), endpoint, runtime)
	if err != nil {
		return httpx.Unavailable("could not query endpoint: " + err.Error())
	}
	return httpx.OK(c, fiber.Map{"models": models})
}

// ------------------------------------------------------------------ prompts

func (h *adminHandler) listPrompts(c *fiber.Ctx) error {
	prompts, err := h.d.store.ListPrompts(c.Context(), c.Query("module"))
	if err != nil {
		return httpx.Internal("failed to list prompts")
	}
	return httpx.OK(c, httpx.ListResponse[store.LLMPrompt]{Items: prompts, Total: len(prompts)})
}

func (h *adminHandler) createPrompt(c *fiber.Ctx) error {
	cur, _ := auth.MustCurrent(c)
	body, err := httpx.Bind[struct {
		Module       string `json:"module"`
		SystemPrompt string `json:"system_prompt"`
		Notes        string `json:"notes"`
		Activate     bool   `json:"activate"`
	}](c)
	if err != nil {
		return err
	}
	if body.Module == "" || body.SystemPrompt == "" {
		return httpx.BadRequest("module and system_prompt are required")
	}
	p, err := h.d.store.CreatePromptVersion(c.Context(), body.Module, body.SystemPrompt, body.Notes, body.Activate, &cur.UserID)
	if err != nil {
		return httpx.Internal("failed to create prompt version")
	}
	h.d.Audit.FromCtx(c, "llm.prompt.version", "llm_prompt", p.ID.String(), audit.SevNotice,
		map[string]any{"module": body.Module, "version": p.Version, "active": body.Activate})
	return httpx.Created(c, p)
}

func (h *adminHandler) activatePrompt(c *fiber.Ctx) error {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		return err
	}
	if err := h.d.store.ActivatePrompt(c.Context(), id); err != nil {
		return httpx.Internal("failed to activate prompt")
	}
	h.d.Audit.FromCtx(c, "llm.prompt.activate", "llm_prompt", id.String(), audit.SevNotice, nil)
	return httpx.NoContent(c)
}

// ------------------------------------------------------------------ egress

func (h *adminHandler) egress(c *fiber.Ctx) error {
	return httpx.OK(c, h.d.Gateway.Assertion())
}

func (h *adminHandler) verifyEgress(c *fiber.Ctx) error {
	a := h.d.Gateway.RefreshAssertion()
	h.d.Audit.FromCtx(c, "llm.egress.verify", "llm_gateway", "", audit.SevNotice,
		map[string]any{"all_private": a.AllPrivate})
	return httpx.OK(c, a)
}

// ------------------------------------------------------------------ targets

func (h *adminHandler) listTargets(c *fiber.Ctx) error {
	cur, _ := auth.MustCurrent(c)
	activeOnly := cur.Role == auth.RoleStudent
	targets, err := h.d.store.ListRangeTargets(c.Context(), activeOnly)
	if err != nil {
		return httpx.Internal("failed to list targets")
	}
	return httpx.OK(c, httpx.ListResponse[store.RangeTarget]{Items: targets, Total: len(targets)})
}

func (h *adminHandler) createTarget(c *fiber.Ctx) error {
	cur, _ := auth.MustCurrent(c)
	body, err := httpx.Bind[struct {
		Slug         string  `json:"slug"`
		Name         string  `json:"name"`
		Description  string  `json:"description"`
		Image        string  `json:"image"`
		ExposedPorts []int32 `json:"exposed_ports"`
		CPUQuota     float64 `json:"cpu_quota"`
		MemoryMB     int     `json:"memory_mb"`
		Privileged   bool    `json:"privileged"`
	}](c)
	if err != nil {
		return err
	}
	if body.Slug == "" || body.Image == "" {
		return httpx.BadRequest("slug and image are required")
	}
	if body.CPUQuota == 0 {
		body.CPUQuota = 1.0
	}
	if body.MemoryMB == 0 {
		body.MemoryMB = 1024
	}
	t, err := h.d.store.CreateRangeTarget(c.Context(), store.RangeTarget{
		Slug: body.Slug, Name: firstNonEmptyStr(body.Name, body.Slug), Description: body.Description,
		Image: body.Image, ExposedPorts: body.ExposedPorts, CPUQuota: body.CPUQuota,
		MemoryMB: body.MemoryMB, Privileged: body.Privileged, CreatedBy: &cur.UserID,
	})
	if err != nil {
		return httpx.Conflict("could not create target (duplicate slug?)")
	}
	h.d.Audit.FromCtx(c, "range.target.provision", "range_target", t.ID.String(), audit.SevNotice,
		map[string]any{"slug": t.Slug, "image": t.Image})
	return httpx.Created(c, t)
}

func (h *adminHandler) setTargetActive(c *fiber.Ctx) error {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		return err
	}
	body, _ := httpx.Bind[struct {
		Active bool `json:"active"`
	}](c)
	if err := h.d.store.SetRangeTargetActive(c.Context(), id, body.Active); err != nil {
		return httpx.Internal("failed to update target")
	}
	return httpx.NoContent(c)
}

func (h *adminHandler) deleteTarget(c *fiber.Ctx) error {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		return err
	}
	if err := h.d.store.DeleteRangeTarget(c.Context(), id); err != nil {
		return httpx.Internal("failed to delete target")
	}
	h.d.Audit.FromCtx(c, "range.target.decommission", "range_target", id.String(), audit.SevWarning, nil)
	return httpx.NoContent(c)
}

// ------------------------------------------------------------------ audit

func (h *adminHandler) auditLog(c *fiber.Ctx) error {
	page := httpx.ParsePage(c, 100, 500)
	f := store.AuditFilter{Action: c.Query("action"), Severity: c.Query("severity"), Limit: page.Limit, Offset: page.Offset}
	if aid, ok := httpx.QueryUUID(c, "actor_id"); ok {
		f.ActorID = &aid
	}
	if v := c.Query("since"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.Since = &t
		}
	}
	if v := c.Query("until"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.Until = &t
		}
	}
	rows, total, err := h.d.store.QueryAudit(c.Context(), f)
	if err != nil {
		return httpx.Internal("failed to query audit log")
	}
	if c.Query("format") == "csv" {
		var b strings.Builder
		b.WriteString("id,created_at,actor,role,action,target_type,target_id,severity,ip\n")
		for _, r := range rows {
			b.WriteString(fmt.Sprintf("%d,%s,%q,%s,%s,%s,%s,%s,%s\n",
				r.ID, r.CreatedAt.Format(time.RFC3339), r.ActorName, r.ActorRole, r.Action, r.TargetType, r.TargetID, r.Severity, r.IP))
		}
		c.Set("Content-Type", "text/csv")
		c.Set("Content-Disposition", `attachment; filename="audit-log.csv"`)
		return c.SendString(b.String())
	}
	return httpx.OK(c, httpx.ListResponse[store.AuditRow]{Items: rows, Total: total})
}

// ------------------------------------------------------------------ health

func (h *adminHandler) systemHealth(c *fiber.Ctx) error {
	activeSessions, _ := h.d.store.CountActiveSessions(c.Context())
	userCount, _ := h.d.authStore.CountUsers(c.Context())
	techCount, _ := h.d.mitreEngine.Count(c.Context())

	rangeOK := false
	rangeErr := ""
	if h.d.provisioner != nil {
		if err := h.d.provisioner.Ping(c.Context()); err != nil {
			rangeErr = err.Error()
		} else {
			rangeOK = true
		}
	} else {
		rangeErr = "provisioner not initialized"
	}

	return httpx.OK(c, fiber.Map{
		"active_sessions": activeSessions,
		"users":           userCount,
		"mitre_techniques": techCount,
		"llm":             h.d.Gateway.Health(c.Context()),
		"range": fiber.Map{
			"driver": func() string {
				if h.d.provisioner != nil {
					return h.d.provisioner.Name()
				}
				return "none"
			}(),
			"ok":    rangeOK,
			"error": rangeErr,
		},
		"redis": h.d.Redis.Ping(c.Context()).Err() == nil,
	})
}

func (h *adminHandler) getSetting(c *fiber.Ctx) error {
	raw, err := h.d.store.GetSetting(c.Context(), c.Params("key"))
	if err != nil {
		return httpx.NotFound("setting not found")
	}
	return c.Type("json").Send(raw)
}

func (h *adminHandler) setSetting(c *fiber.Ctx) error {
	cur, _ := auth.MustCurrent(c)
	key := c.Params("key")
	var value json.RawMessage = c.Body()
	if !json.Valid(value) {
		return httpx.BadRequest("body must be valid JSON")
	}
	if err := h.d.store.SetSetting(c.Context(), key, value, &cur.UserID); err != nil {
		return httpx.Internal("failed to save setting")
	}
	h.d.Audit.FromCtx(c, "settings.update", "platform_setting", key, audit.SevNotice, nil)
	return httpx.NoContent(c)
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

var _ = strconv.Itoa
var _ = uuid.Nil
