package server

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/cyberrange-os/api/internal/audit"
	"github.com/cyberrange-os/api/internal/auth"
	"github.com/cyberrange-os/api/internal/httpx"
	"github.com/cyberrange-os/api/internal/store"
)

type courseHandler struct{ d *Deps }

func newCourseHandler(d *Deps) *courseHandler { return &courseHandler{d: d} }

func (h *courseHandler) register(r fiber.Router) {
	g := r.Group("/courses")
	g.Get("/", h.list)
	g.Get("/:id", h.get)
	g.Post("/", auth.RequireRole(auth.RoleAdmin, auth.RoleFaculty), h.create)
	g.Put("/:id", auth.RequireRole(auth.RoleAdmin, auth.RoleFaculty), h.update)
	g.Delete("/:id", auth.RequireRole(auth.RoleAdmin), h.delete)

	// Course outcomes + PO matrix
	g.Get("/:id/outcomes", h.listCOs)
	g.Post("/:id/outcomes", auth.RequireRole(auth.RoleAdmin, auth.RoleFaculty), h.createCO)
	g.Put("/outcomes/:coid/po-matrix", auth.RequireRole(auth.RoleAdmin, auth.RoleFaculty), h.setPOMatrix)
	g.Delete("/outcomes/:coid", auth.RequireRole(auth.RoleAdmin, auth.RoleFaculty), h.deleteCO)
}

func (h *courseHandler) list(c *fiber.Ctx) error {
	courses, err := h.d.store.ListCourses(c.Context())
	if err != nil {
		return httpx.Internal("failed to list courses")
	}
	return httpx.OK(c, httpx.ListResponse[store.Course]{Items: courses, Total: len(courses)})
}

func (h *courseHandler) get(c *fiber.Ctx) error {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		return err
	}
	course, err := h.d.store.GetCourse(c.Context(), id)
	if err != nil {
		return httpx.NotFound("course not found")
	}
	return httpx.OK(c, course)
}

type courseInput struct {
	Code         string `json:"code"`
	Name         string `json:"name"`
	Semester     int    `json:"semester"`
	AcademicYear string `json:"academic_year"`
}

func (h *courseHandler) create(c *fiber.Ctx) error {
	body, err := httpx.Bind[courseInput](c)
	if err != nil {
		return err
	}
	if body.Code == "" || body.Name == "" {
		return httpx.BadRequest("code and name are required")
	}
	course, err := h.d.store.CreateCourse(c.Context(), body.Code, body.Name, body.Semester, body.AcademicYear)
	if err != nil {
		return httpx.Conflict("could not create course (duplicate code?)")
	}
	h.d.Audit.FromCtx(c, "course.create", "course", course.ID.String(), audit.SevNotice,
		map[string]any{"code": course.Code})
	return httpx.Created(c, course)
}

func (h *courseHandler) update(c *fiber.Ctx) error {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		return err
	}
	body, err := httpx.Bind[courseInput](c)
	if err != nil {
		return err
	}
	if err := h.d.store.UpdateCourse(c.Context(), id, body.Name, body.Semester, body.AcademicYear); err != nil {
		return httpx.Internal("failed to update course")
	}
	h.d.Audit.FromCtx(c, "course.update", "course", id.String(), audit.SevNotice, nil)
	return httpx.NoContent(c)
}

func (h *courseHandler) delete(c *fiber.Ctx) error {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		return err
	}
	if err := h.d.store.DeleteCourse(c.Context(), id); err != nil {
		return httpx.Internal("failed to delete course")
	}
	h.d.Audit.FromCtx(c, "course.delete", "course", id.String(), audit.SevWarning, nil)
	return httpx.NoContent(c)
}

func (h *courseHandler) listCOs(c *fiber.Ctx) error {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		return err
	}
	cos, err := h.d.store.ListCOs(c.Context(), id)
	if err != nil {
		return httpx.Internal("failed to list course outcomes")
	}
	return httpx.OK(c, httpx.ListResponse[store.CourseOutcome]{Items: cos, Total: len(cos)})
}

type coInput struct {
	Code          string  `json:"code"`
	Description   string  `json:"description"`
	TargetPercent float64 `json:"target_percent"`
}

func (h *courseHandler) createCO(c *fiber.Ctx) error {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		return err
	}
	body, err := httpx.Bind[coInput](c)
	if err != nil {
		return err
	}
	if body.Code == "" {
		return httpx.BadRequest("CO code is required")
	}
	if body.TargetPercent == 0 {
		body.TargetPercent = 60
	}
	co, err := h.d.store.CreateCO(c.Context(), id, body.Code, body.Description, body.TargetPercent)
	if err != nil {
		return httpx.Conflict("could not create outcome (duplicate code?)")
	}
	return httpx.Created(c, co)
}

func (h *courseHandler) setPOMatrix(c *fiber.Ctx) error {
	coID, err := httpx.ParamUUID(c, "coid")
	if err != nil {
		return err
	}
	body, err := httpx.Bind[struct {
		Mappings []store.POMapping `json:"mappings"`
	}](c)
	if err != nil {
		return err
	}
	if err := h.d.store.SetPOMatrix(c.Context(), coID, body.Mappings); err != nil {
		return httpx.Internal("failed to set PO matrix")
	}
	h.d.Audit.FromCtx(c, "copo.matrix.update", "course_outcome", coID.String(), audit.SevNotice,
		map[string]any{"mappings": len(body.Mappings)})
	return httpx.NoContent(c)
}

func (h *courseHandler) deleteCO(c *fiber.Ctx) error {
	coID, err := httpx.ParamUUID(c, "coid")
	if err != nil {
		return err
	}
	if err := h.d.store.DeleteCO(c.Context(), coID); err != nil {
		return httpx.Internal("failed to delete outcome")
	}
	return httpx.NoContent(c)
}

var _ = uuid.Nil
