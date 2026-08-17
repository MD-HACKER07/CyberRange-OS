package server

import (
	"encoding/json"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/cyberrange-os/api/internal/audit"
	"github.com/cyberrange-os/api/internal/auth"
	"github.com/cyberrange-os/api/internal/httpx"
	"github.com/cyberrange-os/api/internal/store"
)

type exerciseHandler struct{ d *Deps }

func newExerciseHandler(d *Deps) *exerciseHandler { return &exerciseHandler{d: d} }

func (h *exerciseHandler) register(r fiber.Router) {
	g := r.Group("/exercises")
	g.Get("/", h.list)
	g.Get("/:id", h.get)
	g.Post("/", auth.RequireRole(auth.RoleAdmin, auth.RoleFaculty), h.create)
	g.Put("/:id", auth.RequireRole(auth.RoleAdmin, auth.RoleFaculty), h.update)
	g.Post("/:id/publish", auth.RequireRole(auth.RoleAdmin, auth.RoleFaculty), h.publish)
	g.Delete("/:id", auth.RequireRole(auth.RoleAdmin, auth.RoleFaculty), h.delete)
}

func (h *exerciseHandler) list(c *fiber.Ctx) error {
	batchID, ok := httpx.QueryUUID(c, "batch_id")
	if !ok {
		return httpx.BadRequest("batch_id query parameter is required")
	}
	cur, _ := auth.MustCurrent(c)
	publishedOnly := cur.Role == auth.RoleStudent
	items, err := h.d.store.ListExercises(c.Context(), batchID, publishedOnly, c.Query("type"))
	if err != nil {
		return httpx.Internal("failed to list exercises")
	}
	return httpx.OK(c, httpx.ListResponse[store.Exercise]{Items: items, Total: len(items)})
}

func (h *exerciseHandler) get(c *fiber.Ctx) error {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		return err
	}
	ex, err := h.d.store.GetExercise(c.Context(), id)
	if err != nil {
		return httpx.NotFound("exercise not found")
	}
	cur, _ := auth.MustCurrent(c)
	if cur.Role == auth.RoleStudent && !ex.IsPublished {
		return httpx.NotFound("exercise not found")
	}
	return httpx.OK(c, ex)
}

type exerciseInput struct {
	BatchID            string          `json:"batch_id"`
	Type               string          `json:"type"`
	Title              string          `json:"title"`
	BriefMD            string          `json:"brief_md"`
	RubricJSON         json.RawMessage `json:"rubric_json"`
	Difficulty         int             `json:"difficulty"`
	COIDs              []string        `json:"co_ids"`
	CertObjectiveCodes []string        `json:"cert_objective_codes"`
	TargetImageRefs    []string        `json:"target_image_refs"`
	ExpectedTechniques []string        `json:"expected_techniques"`
	PairedExerciseID   *string         `json:"paired_exercise_id"`
	AIRedTeamEnabled   bool            `json:"ai_redteam_enabled"`
	TimeLimitMinutes   int             `json:"time_limit_minutes"`
}

func (in exerciseInput) toStore(createdBy *uuid.UUID) (store.ExerciseInput, error) {
	out := store.ExerciseInput{
		Type:               in.Type,
		Title:              in.Title,
		BriefMD:            in.BriefMD,
		RubricJSON:         in.RubricJSON,
		Difficulty:         in.Difficulty,
		CertObjectiveCodes: in.CertObjectiveCodes,
		TargetImageRefs:    in.TargetImageRefs,
		ExpectedTechniques: in.ExpectedTechniques,
		AIRedTeamEnabled:   in.AIRedTeamEnabled,
		TimeLimitMinutes:   in.TimeLimitMinutes,
		CreatedBy:          createdBy,
	}
	bid, err := uuid.Parse(in.BatchID)
	if err != nil {
		return out, httpx.BadRequest("valid batch_id is required")
	}
	out.BatchID = bid
	for _, raw := range in.COIDs {
		if id, err := uuid.Parse(raw); err == nil {
			out.COIDs = append(out.COIDs, id)
		}
	}
	if in.PairedExerciseID != nil && *in.PairedExerciseID != "" {
		if pid, err := uuid.Parse(*in.PairedExerciseID); err == nil {
			out.PairedExerciseID = &pid
		}
	}
	return out, nil
}

func (h *exerciseHandler) create(c *fiber.Ctx) error {
	body, err := httpx.Bind[exerciseInput](c)
	if err != nil {
		return err
	}
	if body.Type != "red" && body.Type != "blue" {
		return httpx.BadRequest("type must be 'red' or 'blue'")
	}
	if body.Title == "" {
		return httpx.BadRequest("title is required")
	}
	cur, _ := auth.MustCurrent(c)
	if err := h.authorizeBatch(c, body.BatchID); err != nil {
		return err
	}
	uid := cur.UserID
	in, err := body.toStore(&uid)
	if err != nil {
		return err
	}
	ex, err := h.d.store.CreateExercise(c.Context(), in)
	if err != nil {
		return httpx.Internal("failed to create exercise: " + err.Error())
	}
	h.d.Audit.FromCtx(c, "exercise.create", "exercise", ex.ID.String(), audit.SevNotice,
		map[string]any{"title": ex.Title, "type": ex.Type})
	return httpx.Created(c, ex)
}

func (h *exerciseHandler) update(c *fiber.Ctx) error {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		return err
	}
	body, err := httpx.Bind[exerciseInput](c)
	if err != nil {
		return err
	}
	in, err := body.toStore(nil)
	if err != nil {
		return err
	}
	ex, err := h.d.store.UpdateExercise(c.Context(), id, in)
	if err != nil {
		return httpx.Internal("failed to update exercise")
	}
	h.d.Audit.FromCtx(c, "exercise.update", "exercise", id.String(), audit.SevInfo, nil)
	return httpx.OK(c, ex)
}

func (h *exerciseHandler) publish(c *fiber.Ctx) error {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		return err
	}
	body, _ := httpx.Bind[struct {
		Published bool `json:"published"`
	}](c)
	if err := h.d.store.SetPublished(c.Context(), id, body.Published); err != nil {
		return httpx.Internal("failed to change publish state")
	}
	h.d.Audit.FromCtx(c, "exercise.publish", "exercise", id.String(), audit.SevInfo,
		map[string]any{"published": body.Published})
	return httpx.NoContent(c)
}

func (h *exerciseHandler) delete(c *fiber.Ctx) error {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		return err
	}
	if err := h.d.store.DeleteExercise(c.Context(), id); err != nil {
		return httpx.Internal("failed to delete exercise")
	}
	h.d.Audit.FromCtx(c, "exercise.delete", "exercise", id.String(), audit.SevWarning, nil)
	return httpx.NoContent(c)
}

func (h *exerciseHandler) authorizeBatch(c *fiber.Ctx, batchIDRaw string) error {
	cur, err := auth.MustCurrent(c)
	if err != nil {
		return err
	}
	if cur.Role == auth.RoleAdmin {
		return nil
	}
	bid, err := uuid.Parse(batchIDRaw)
	if err != nil {
		return httpx.BadRequest("valid batch_id required")
	}
	ok, err := h.d.store.FacultyOwnsBatch(c.Context(), bid, cur.UserID)
	if err != nil {
		return httpx.Internal("authorization check failed")
	}
	if !ok {
		return httpx.Forbidden("you do not manage this batch")
	}
	return nil
}
