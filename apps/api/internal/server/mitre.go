package server

import (
	"github.com/gofiber/fiber/v2"

	"github.com/cyberrange-os/api/internal/httpx"
	"github.com/cyberrange-os/api/internal/mitre"
)

type mitreHandler struct{ d *Deps }

func newMitreHandler(d *Deps) *mitreHandler { return &mitreHandler{d: d} }

func (h *mitreHandler) register(r fiber.Router) {
	g := r.Group("/attack")
	g.Get("/techniques", h.list)
	g.Get("/techniques/:tid", h.get)
	g.Post("/tag", h.tag)
	g.Get("/status", h.status)
}

func (h *mitreHandler) list(c *fiber.Ctx) error {
	page := httpx.ParsePage(c, 200, 500)
	techs, err := h.d.mitreEngine.List(c.Context(), c.Query("tactic"), c.Query("search"), page.Limit)
	if err != nil {
		return httpx.Internal("failed to list techniques")
	}
	return httpx.OK(c, httpx.ListResponse[mitre.Technique]{Items: techs, Total: len(techs)})
}

func (h *mitreHandler) get(c *fiber.Ctx) error {
	tech, err := h.d.mitreEngine.Get(c.Context(), c.Params("tid"))
	if err != nil {
		return httpx.Internal("lookup failed")
	}
	if tech == nil {
		return httpx.NotFound("technique not found")
	}
	return httpx.OK(c, tech)
}

func (h *mitreHandler) tag(c *fiber.Ctx) error {
	body, err := httpx.Bind[struct {
		Text string `json:"text"`
	}](c)
	if err != nil {
		return err
	}
	if body.Text == "" {
		return httpx.BadRequest("text is required")
	}
	tid, conf, err := h.d.mitreEngine.Tag(c.Context(), body.Text)
	if err != nil {
		return httpx.Unavailable("tagging failed: " + err.Error())
	}
	tech, _ := h.d.mitreEngine.Get(c.Context(), tid)
	return httpx.OK(c, fiber.Map{"technique_id": tid, "confidence": conf, "technique": tech})
}

func (h *mitreHandler) status(c *fiber.Ctx) error {
	n, err := h.d.mitreEngine.Count(c.Context())
	if err != nil {
		return httpx.Internal("failed to count techniques")
	}
	return httpx.OK(c, fiber.Map{"technique_count": n, "seeded": n > 0})
}
