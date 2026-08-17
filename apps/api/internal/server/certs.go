package server

import (
	"github.com/gofiber/fiber/v2"

	"github.com/cyberrange-os/api/internal/auth"
	"github.com/cyberrange-os/api/internal/httpx"
	"github.com/cyberrange-os/api/internal/store"
)

type certHandler struct{ d *Deps }

func newCertHandler(d *Deps) *certHandler { return &certHandler{d: d} }

func (h *certHandler) register(r fiber.Router) {
	g := r.Group("/certifications")
	g.Get("/objectives", h.objectives)
	g.Get("/progress", h.myProgress)
	g.Get("/progress/:uid", auth.RequireRole(auth.RoleFaculty, auth.RoleAdmin, auth.RoleAuditor), h.userProgress)
}

func (h *certHandler) objectives(c *fiber.Ctx) error {
	objs, err := h.d.store.ListCertObjectives(c.Context(), c.Query("cert"))
	if err != nil {
		return httpx.Internal("failed to list objectives")
	}
	return httpx.OK(c, httpx.ListResponse[store.CertObjective]{Items: objs, Total: len(objs)})
}

func (h *certHandler) myProgress(c *fiber.Ctx) error {
	cur, _ := auth.MustCurrent(c)
	prog, err := h.d.store.CertProgressForUser(c.Context(), cur.UserID)
	if err != nil {
		return httpx.Internal("failed to compute progress")
	}
	return httpx.OK(c, fiber.Map{"note": "In-house competency mapping. Not an official certification.", "paths": prog})
}

func (h *certHandler) userProgress(c *fiber.Ctx) error {
	uid, err := httpx.ParamUUID(c, "uid")
	if err != nil {
		return err
	}
	prog, err := h.d.store.CertProgressForUser(c.Context(), uid)
	if err != nil {
		return httpx.Internal("failed to compute progress")
	}
	return httpx.OK(c, fiber.Map{"paths": prog})
}
