package server

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/cyberrange-os/api/internal/auth"
	"github.com/cyberrange-os/api/internal/httpx"
	"github.com/cyberrange-os/api/internal/store"
)

type analyticsHandler struct{ d *Deps }

func newAnalyticsHandler(d *Deps) *analyticsHandler { return &analyticsHandler{d: d} }

func (h *analyticsHandler) register(r fiber.Router) {
	g := r.Group("/attainment", auth.RequireRole(auth.RoleFaculty, auth.RoleAdmin, auth.RoleAuditor))
	g.Get("/batch/:bid", h.batchAttainment)
	g.Get("/batch/:bid/po", h.poAttainment)
	g.Get("/export/:bid", h.export)

	naac := r.Group("/naac", auth.RequireRole(auth.RoleAdmin, auth.RoleAuditor, auth.RoleFaculty))
	naac.Get("/evidence", h.naac)

	surveys := r.Group("/surveys")
	surveys.Get("/", h.listSurveys)
	surveys.Post("/", auth.RequireRole(auth.RoleFaculty, auth.RoleAdmin), h.createSurvey)
	surveys.Post("/:id/respond", h.respondSurvey)
}

func (h *analyticsHandler) attInput(bid uuid.UUID) store.AttainmentInput {
	return store.AttainmentInput{BatchID: bid, DirectWeight: h.d.Cfg.DirectWeight, IndirectWeight: h.d.Cfg.IndirectWeight}
}

func (h *analyticsHandler) batchAttainment(c *fiber.Ctx) error {
	bid, err := httpx.ParamUUID(c, "bid")
	if err != nil {
		return err
	}
	att, err := h.d.store.ComputeAndStoreAttainment(c.Context(), h.attInput(bid))
	if err != nil {
		return httpx.Internal("failed to compute attainment: " + err.Error())
	}
	return httpx.OK(c, fiber.Map{
		"batch_id":        bid,
		"direct_weight":   h.d.Cfg.DirectWeight,
		"indirect_weight": h.d.Cfg.IndirectWeight,
		"outcomes":        att,
	})
}

func (h *analyticsHandler) poAttainment(c *fiber.Ctx) error {
	bid, err := httpx.ParamUUID(c, "bid")
	if err != nil {
		return err
	}
	co, err := h.d.store.ComputeAndStoreAttainment(c.Context(), h.attInput(bid))
	if err != nil {
		return httpx.Internal("failed to compute attainment")
	}
	po, err := h.d.store.ComputePOAttainment(c.Context(), co)
	if err != nil {
		return httpx.Internal("failed to roll up to PO")
	}
	return httpx.OK(c, fiber.Map{"co": co, "po": po})
}

// export produces an NBA-format attainment CSV.
func (h *analyticsHandler) export(c *fiber.Ctx) error {
	bid, err := httpx.ParamUUID(c, "bid")
	if err != nil {
		return err
	}
	co, err := h.d.store.ComputeAndStoreAttainment(c.Context(), h.attInput(bid))
	if err != nil {
		return httpx.Internal("failed to compute attainment")
	}
	po, _ := h.d.store.ComputePOAttainment(c.Context(), co)
	batch, _ := h.d.store.GetBatch(c.Context(), bid)

	var b strings.Builder
	b.WriteString("CyberRange OS — NBA CO/PO Attainment Report\n")
	if batch != nil {
		b.WriteString(fmt.Sprintf("Course,%s - %s\nBatch,%s\n\n", batch.CourseCode, batch.CourseName, batch.Name))
	}
	b.WriteString("CO Code,Description,Target %,Direct %,Indirect %,Final %,Attainment Level,Samples\n")
	for _, o := range co {
		b.WriteString(fmt.Sprintf("%s,%q,%.2f,%.2f,%.2f,%.2f,%d,%d\n",
			o.COCode, o.Description, o.TargetPercent, o.DirectScore, o.IndirectScore, o.FinalScore, o.AttainmentLvl, o.SampleSize))
	}
	b.WriteString("\nPO Code,Attainment %,Total Weight,Contributing COs\n")
	for _, p := range po {
		b.WriteString(fmt.Sprintf("%s,%.2f,%.2f,%d\n", p.POCode, p.Score, p.Weight, p.Contribs))
	}
	c.Set("Content-Type", "text/csv")
	c.Set("Content-Disposition", `attachment; filename="attainment-report.csv"`)
	return c.SendString(b.String())
}

func (h *analyticsHandler) naac(c *fiber.Ctx) error {
	stats, err := h.d.store.NAACStats(c.Context())
	if err != nil {
		return httpx.Internal("failed to compile NAAC stats")
	}
	if c.Query("format") == "csv" {
		var b strings.Builder
		b.WriteString("Metric,Value\n")
		b.WriteString(fmt.Sprintf("Total Students,%d\n", stats.TotalStudents))
		b.WriteString(fmt.Sprintf("Total Exercises,%d\n", stats.TotalExercises))
		b.WriteString(fmt.Sprintf("Range Sessions,%d\n", stats.RangeSessions))
		b.WriteString(fmt.Sprintf("Reports Graded,%d\n", stats.ReportsGraded))
		b.WriteString(fmt.Sprintf("AI Security Findings,%d\n", stats.AISecurityFindings))
		b.WriteString(fmt.Sprintf("AI Calls Logged,%d\n", stats.AICallsLogged))
		c.Set("Content-Type", "text/csv")
		c.Set("Content-Disposition", `attachment; filename="naac-evidence.csv"`)
		return c.SendString(b.String())
	}
	return httpx.OK(c, stats)
}

// ------------------------------------------------------------------ surveys

func (h *analyticsHandler) listSurveys(c *fiber.Ctx) error {
	bid, ok := httpx.QueryUUID(c, "batch_id")
	if !ok {
		return httpx.BadRequest("batch_id is required")
	}
	surveys, err := h.d.store.ListSurveys(c.Context(), bid)
	if err != nil {
		return httpx.Internal("failed to list surveys")
	}
	return httpx.OK(c, httpx.ListResponse[store.Survey]{Items: surveys, Total: len(surveys)})
}

func (h *analyticsHandler) createSurvey(c *fiber.Ctx) error {
	cur, _ := auth.MustCurrent(c)
	body, err := httpx.Bind[struct {
		BatchID   string          `json:"batch_id"`
		Title     string          `json:"title"`
		COIDs     []string        `json:"co_ids"`
		Questions json.RawMessage `json:"questions_json"`
	}](c)
	if err != nil {
		return err
	}
	bid, err := uuid.Parse(body.BatchID)
	if err != nil {
		return httpx.BadRequest("valid batch_id required")
	}
	coIDs := []uuid.UUID{}
	for _, raw := range body.COIDs {
		if id, err := uuid.Parse(raw); err == nil {
			coIDs = append(coIDs, id)
		}
	}
	sv, err := h.d.store.CreateSurvey(c.Context(), bid, body.Title, coIDs, body.Questions, &cur.UserID)
	if err != nil {
		return httpx.Internal("failed to create survey")
	}
	return httpx.Created(c, sv)
}

func (h *analyticsHandler) respondSurvey(c *fiber.Ctx) error {
	cur, _ := auth.MustCurrent(c)
	sid, err := httpx.ParamUUID(c, "id")
	if err != nil {
		return err
	}
	body, err := httpx.Bind[struct {
		Answers json.RawMessage `json:"answers_json"`
	}](c)
	if err != nil {
		return err
	}
	if err := h.d.store.SubmitSurveyResponse(c.Context(), sid, cur.UserID, body.Answers); err != nil {
		return httpx.Internal("failed to submit response")
	}
	return httpx.NoContent(c)
}
