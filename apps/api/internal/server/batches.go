package server

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/cyberrange-os/api/internal/audit"
	"github.com/cyberrange-os/api/internal/auth"
	"github.com/cyberrange-os/api/internal/httpx"
	"github.com/cyberrange-os/api/internal/store"
)

type batchHandler struct{ d *Deps }

func newBatchHandler(d *Deps) *batchHandler { return &batchHandler{d: d} }

func (h *batchHandler) register(r fiber.Router) {
	g := r.Group("/batches")
	g.Get("/", h.list)
	g.Get("/:id", h.get)
	g.Get("/:id/students", h.students)
	g.Post("/", auth.RequireRole(auth.RoleAdmin, auth.RoleFaculty), h.create)
	g.Put("/:id/faculty", auth.RequireRole(auth.RoleAdmin), h.assignFaculty)
	g.Post("/:id/enroll", auth.RequireRole(auth.RoleAdmin, auth.RoleFaculty), h.enroll)
	g.Delete("/:id/enroll/:uid", auth.RequireRole(auth.RoleAdmin, auth.RoleFaculty), h.unenroll)
	g.Delete("/:id", auth.RequireRole(auth.RoleAdmin), h.delete)
}

func (h *batchHandler) list(c *fiber.Ctx) error {
	id, _ := auth.MustCurrent(c)
	var courseID *uuid.UUID
	if cid, ok := httpx.QueryUUID(c, "course_id"); ok {
		courseID = &cid
	}
	var facultyID *uuid.UUID
	// Faculty see only their batches unless they pass all=1 (still their own).
	if id.Role == auth.RoleFaculty {
		f := id.UserID
		facultyID = &f
	}
	batches, err := h.d.store.ListBatches(c.Context(), courseID, facultyID)
	if err != nil {
		return httpx.Internal("failed to list batches")
	}
	return httpx.OK(c, httpx.ListResponse[store.Batch]{Items: batches, Total: len(batches)})
}

func (h *batchHandler) get(c *fiber.Ctx) error {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		return err
	}
	b, err := h.d.store.GetBatch(c.Context(), id)
	if err != nil {
		return httpx.NotFound("batch not found")
	}
	return httpx.OK(c, b)
}

func (h *batchHandler) students(c *fiber.Ctx) error {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		return err
	}
	if err := h.authorizeBatchStaff(c, id); err != nil {
		return err
	}
	members, err := h.d.store.BatchStudents(c.Context(), id)
	if err != nil {
		return httpx.Internal("failed to list students")
	}
	return httpx.OK(c, httpx.ListResponse[store.BatchMember]{Items: members, Total: len(members)})
}

type batchInput struct {
	CourseID  string  `json:"course_id"`
	FacultyID *string `json:"faculty_id"`
	Name      string  `json:"name"`
	Term      string  `json:"term"`
}

func (h *batchHandler) create(c *fiber.Ctx) error {
	body, err := httpx.Bind[batchInput](c)
	if err != nil {
		return err
	}
	courseID, err := uuid.Parse(body.CourseID)
	if err != nil {
		return httpx.BadRequest("valid course_id is required")
	}
	if body.Name == "" {
		return httpx.BadRequest("batch name is required")
	}
	var faculty *uuid.UUID
	cur, _ := auth.MustCurrent(c)
	if body.FacultyID != nil && *body.FacultyID != "" {
		f, err := uuid.Parse(*body.FacultyID)
		if err != nil {
			return httpx.BadRequest("invalid faculty_id")
		}
		faculty = &f
	} else if cur.Role == auth.RoleFaculty {
		f := cur.UserID
		faculty = &f
	}
	b, err := h.d.store.CreateBatch(c.Context(), courseID, faculty, body.Name, body.Term)
	if err != nil {
		return httpx.Conflict("could not create batch (duplicate name?)")
	}
	h.d.Audit.FromCtx(c, "batch.create", "batch", b.ID.String(), audit.SevNotice, map[string]any{"name": b.Name})
	return httpx.Created(c, b)
}

func (h *batchHandler) assignFaculty(c *fiber.Ctx) error {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		return err
	}
	body, err := httpx.Bind[struct {
		FacultyID string `json:"faculty_id"`
	}](c)
	if err != nil {
		return err
	}
	fid, err := uuid.Parse(body.FacultyID)
	if err != nil {
		return httpx.BadRequest("valid faculty_id required")
	}
	if err := h.d.store.AssignFaculty(c.Context(), id, fid); err != nil {
		return httpx.Internal("failed to assign faculty")
	}
	h.d.Audit.FromCtx(c, "batch.assign_faculty", "batch", id.String(), audit.SevNotice,
		map[string]any{"faculty_id": fid.String()})
	return httpx.NoContent(c)
}

func (h *batchHandler) enroll(c *fiber.Ctx) error {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		return err
	}
	if err := h.authorizeBatchStaff(c, id); err != nil {
		return err
	}
	body, err := httpx.Bind[struct {
		UserIDs []string `json:"user_ids"`
	}](c)
	if err != nil {
		return err
	}
	enrolled := 0
	for _, raw := range body.UserIDs {
		uid, err := uuid.Parse(raw)
		if err != nil {
			continue
		}
		if err := h.d.store.Enroll(c.Context(), id, uid); err == nil {
			enrolled++
		}
	}
	h.d.Audit.FromCtx(c, "batch.enroll", "batch", id.String(), audit.SevInfo, map[string]any{"count": enrolled})
	return httpx.OK(c, fiber.Map{"enrolled": enrolled})
}

func (h *batchHandler) unenroll(c *fiber.Ctx) error {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		return err
	}
	if err := h.authorizeBatchStaff(c, id); err != nil {
		return err
	}
	uid, err := httpx.ParamUUID(c, "uid")
	if err != nil {
		return err
	}
	if err := h.d.store.Unenroll(c.Context(), id, uid); err != nil {
		return httpx.Internal("failed to unenroll")
	}
	return httpx.NoContent(c)
}

func (h *batchHandler) delete(c *fiber.Ctx) error {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		return err
	}
	if err := h.d.store.DeleteBatch(c.Context(), id); err != nil {
		return httpx.Internal("failed to delete batch")
	}
	h.d.Audit.FromCtx(c, "batch.delete", "batch", id.String(), audit.SevWarning, nil)
	return httpx.NoContent(c)
}

// authorizeBatchStaff allows admins for any batch, faculty only for batches
// they own.
func (h *batchHandler) authorizeBatchStaff(c *fiber.Ctx, batchID uuid.UUID) error {
	cur, err := auth.MustCurrent(c)
	if err != nil {
		return err
	}
	if cur.Role == auth.RoleAdmin {
		return nil
	}
	if cur.Role == auth.RoleFaculty {
		ok, err := h.d.store.FacultyOwnsBatch(c.Context(), batchID, cur.UserID)
		if err != nil {
			return httpx.Internal("authorization check failed")
		}
		if ok {
			return nil
		}
	}
	return httpx.Forbidden("you do not manage this batch")
}
