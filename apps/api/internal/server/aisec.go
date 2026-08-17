package server

import (
	"github.com/gofiber/fiber/v2"

	"github.com/cyberrange-os/api/internal/aisec"
	"github.com/cyberrange-os/api/internal/audit"
	"github.com/cyberrange-os/api/internal/auth"
	"github.com/cyberrange-os/api/internal/httpx"
	"github.com/cyberrange-os/api/internal/store"
)

type aisecHandler struct {
	d      *Deps
	runner *aisec.Runner
}

func newAISecHandler(d *Deps) *aisecHandler {
	return &aisecHandler{d: d, runner: aisec.NewRunner(d.Cfg, d.store, d.Gateway, d.Log)}
}

func (h *aisecHandler) register(r fiber.Router) {
	g := r.Group("/ai-security")
	g.Get("/results", h.results)
	g.Post("/scan", auth.RequireRole(auth.RoleFaculty, auth.RoleAdmin), h.scan)
}

func (h *aisecHandler) results(c *fiber.Ctx) error {
	scans, err := h.d.store.ListAIScans(c.Context(), 100)
	if err != nil {
		return httpx.Internal("failed to list scans")
	}
	return httpx.OK(c, httpx.ListResponse[store.AIScan]{Items: scans, Total: len(scans)})
}

func (h *aisecHandler) scan(c *fiber.Ctx) error {
	cur, _ := auth.MustCurrent(c)
	body, _ := httpx.Bind[struct {
		Tool         string `json:"tool"`
		Model        string `json:"model"`
		Endpoint     string `json:"endpoint"`
		PromptModule string `json:"prompt_module"`
	}](c)
	if body.Tool != "pyrit" && body.Tool != "garak" {
		body.Tool = "garak"
	}
	if body.Model == "" {
		body.Model = h.d.Cfg.LLMDefaultModel
	}
	if body.PromptModule == "" {
		body.PromptModule = "red-teaming-target"
	}
	// The endpoint is validated as local/registered inside the runner; the UI
	// only ever offers registered endpoints (locked, not free text).
	scanID, err := h.runner.Run(c.Context(), body.Tool, body.Model, body.Endpoint, body.PromptModule, &cur.UserID)
	if err != nil {
		return httpx.BadRequest(err.Error())
	}
	h.d.Audit.FromCtx(c, "aisec.scan.start", "ai_redteam_scan", scanID.String(), audit.SevNotice,
		map[string]any{"tool": body.Tool, "model": body.Model})
	return httpx.Created(c, fiber.Map{"scan_id": scanID, "status": "running"})
}
