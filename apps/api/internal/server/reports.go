package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/cyberrange-os/api/internal/audit"
	"github.com/cyberrange-os/api/internal/auth"
	"github.com/cyberrange-os/api/internal/httpx"
	"github.com/cyberrange-os/api/internal/llm"
	"github.com/cyberrange-os/api/internal/report"
	"github.com/cyberrange-os/api/internal/store"
)

type reportHandler struct {
	d        *Deps
	renderer *report.Renderer
}

func newReportHandler(d *Deps) *reportHandler {
	return &reportHandler{d: d, renderer: report.NewRenderer(d.Cfg.PDFBinary, d.Cfg.PDFRenderer, d.Cfg.StorageDir)}
}

func (h *reportHandler) register(r fiber.Router) {
	g := r.Group("/reports")
	g.Get("/", h.list)
	g.Get("/grading-queue", auth.RequireRole(auth.RoleFaculty, auth.RoleAdmin), h.gradingQueue)
	g.Post("/", h.create)
	g.Post("/from-session/:sid", h.fromSession)
	g.Get("/:id", h.get)
	g.Put("/:id", h.update)
	g.Post("/:id/submit", h.submit)
	g.Post("/:id/ai-grade", auth.RequireRole(auth.RoleFaculty, auth.RoleAdmin), h.aiGrade)
	g.Post("/:id/grade", auth.RequireRole(auth.RoleFaculty, auth.RoleAdmin), h.grade)
	g.Get("/:id/pdf", h.pdf)
	g.Get("/portfolio/:uid", h.portfolio)
}

func (h *reportHandler) loadOwned(c *fiber.Ctx) (*store.Report, error) {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		return nil, err
	}
	rep, err := h.d.store.GetReport(c.Context(), id)
	if err != nil {
		return nil, httpx.NotFound("report not found")
	}
	cur, _ := auth.MustCurrent(c)
	if cur.Role == auth.RoleStudent && rep.UserID != cur.UserID {
		return nil, httpx.Forbidden("not your report")
	}
	return rep, nil
}

func (h *reportHandler) list(c *fiber.Ctx) error {
	cur, _ := auth.MustCurrent(c)
	page := httpx.ParsePage(c, 30, 100)
	var userFilter *uuid.UUID
	if cur.Role == auth.RoleStudent {
		userFilter = &cur.UserID
	} else if uid, ok := httpx.QueryUUID(c, "user_id"); ok {
		userFilter = &uid
	}
	var exFilter *uuid.UUID
	if eid, ok := httpx.QueryUUID(c, "exercise_id"); ok {
		exFilter = &eid
	}
	items, total, err := h.d.store.ListReports(c.Context(), userFilter, exFilter, c.Query("status"), page.Limit, page.Offset)
	if err != nil {
		return httpx.Internal("failed to list reports")
	}
	return httpx.OK(c, httpx.ListResponse[store.Report]{Items: items, Total: total})
}

func (h *reportHandler) gradingQueue(c *fiber.Ctx) error {
	cur, _ := auth.MustCurrent(c)
	items, err := h.d.store.GradingQueue(c.Context(), cur.UserID, cur.Role == auth.RoleAdmin)
	if err != nil {
		return httpx.Internal("failed to load grading queue")
	}
	return httpx.OK(c, httpx.ListResponse[store.Report]{Items: items, Total: len(items)})
}

type reportInput struct {
	SessionID  string          `json:"session_id"`
	ExerciseID string          `json:"exercise_id"`
	Type       string          `json:"type"`
	Title      string          `json:"title"`
	ContentMD  string          `json:"content_md"`
	Techniques []string        `json:"technique_ids"`
}

func (h *reportHandler) create(c *fiber.Ctx) error {
	cur, _ := auth.MustCurrent(c)
	body, err := httpx.Bind[reportInput](c)
	if err != nil {
		return err
	}
	if body.Type != "pentest" && body.Type != "incident" {
		return httpx.BadRequest("type must be pentest or incident")
	}
	in := store.ReportInput{UserID: cur.UserID, Type: body.Type, Title: body.Title, ContentMD: body.ContentMD, TechniqueIDs: body.Techniques}
	if body.SessionID != "" {
		if sid, err := uuid.Parse(body.SessionID); err == nil {
			in.SessionID = &sid
		}
	}
	if body.ExerciseID != "" {
		if eid, err := uuid.Parse(body.ExerciseID); err == nil {
			in.ExerciseID = &eid
		}
	}
	rep, err := h.d.store.CreateReport(c.Context(), in)
	if err != nil {
		return httpx.Internal("failed to create report")
	}
	return httpx.Created(c, rep)
}

// fromSession pre-populates a report with the MITRE-tagged command timeline.
func (h *reportHandler) fromSession(c *fiber.Ctx) error {
	cur, _ := auth.MustCurrent(c)
	sid, err := httpx.ParamUUID(c, "sid")
	if err != nil {
		return err
	}
	sess, err := h.d.store.GetSession(c.Context(), sid)
	if err != nil {
		return httpx.NotFound("session not found")
	}
	if cur.Role == auth.RoleStudent && sess.UserID != cur.UserID {
		return httpx.Forbidden("not your session")
	}
	ex, _ := h.d.store.GetExercise(c.Context(), sess.ExerciseID)
	log, _ := h.d.store.CommandLog(c.Context(), sess.ID)
	techniques, _ := h.d.store.SessionTechniques(c.Context(), sess.ID)

	content := buildSessionReportTemplate(ex, sess, log)
	title := "Penetration Test Report"
	if ex != nil {
		title = ex.Title + " — Pentest Report"
	}
	rep, err := h.d.store.CreateReport(c.Context(), store.ReportInput{
		SessionID: &sess.ID, ExerciseID: &sess.ExerciseID, UserID: sess.UserID,
		Type: "pentest", Title: title, ContentMD: content, TechniqueIDs: techniques,
	})
	if err != nil {
		return httpx.Internal("failed to create report")
	}
	return httpx.Created(c, rep)
}

func buildSessionReportTemplate(ex *store.Exercise, sess *store.RangeSession, log []store.CommandLogEntry) string {
	var sb strings.Builder
	sb.WriteString("# Penetration Test Report\n\n")
	if ex != nil {
		sb.WriteString("## Scenario\n" + ex.BriefMD + "\n\n")
	}
	sb.WriteString("## Executive Summary\n_Summarize your findings here._\n\n")
	sb.WriteString("## Methodology & Timeline\n\n")
	for _, e := range log {
		tech := ""
		if e.MitreTechniqueID != nil && *e.MitreTechniqueID != "" {
			tech = fmt.Sprintf(" _(MITRE %s)_", *e.MitreTechniqueID)
		}
		src := "manual"
		if e.WasAISuggested {
			src = "copilot-assisted"
		}
		sb.WriteString(fmt.Sprintf("### Step %d — %s%s\n", e.Seq, src, tech))
		sb.WriteString("```\n$ " + e.Command + "\n")
		out := e.Output
		if len(out) > 1200 {
			out = out[:1200] + "\n...(truncated)"
		}
		sb.WriteString(out + "\n```\n\n")
	}
	sb.WriteString("## Findings & Remediation\n_List vulnerabilities discovered and recommended fixes._\n\n")
	sb.WriteString("## Conclusion\n_Wrap up._\n")
	return sb.String()
}

func (h *reportHandler) get(c *fiber.Ctx) error {
	rep, err := h.loadOwned(c)
	if err != nil {
		return err
	}
	return httpx.OK(c, rep)
}

func (h *reportHandler) update(c *fiber.Ctx) error {
	rep, err := h.loadOwned(c)
	if err != nil {
		return err
	}
	body, err := httpx.Bind[reportInput](c)
	if err != nil {
		return err
	}
	updated, err := h.d.store.UpdateReportContent(c.Context(), rep.ID, body.Title, body.ContentMD, body.Techniques)
	if err != nil {
		return httpx.Conflict("report cannot be edited in its current status")
	}
	return httpx.OK(c, updated)
}

func (h *reportHandler) submit(c *fiber.Ctx) error {
	rep, err := h.loadOwned(c)
	if err != nil {
		return err
	}
	if err := h.d.store.SubmitReport(c.Context(), rep.ID); err != nil {
		return httpx.Internal("failed to submit report")
	}
	h.d.Audit.FromCtx(c, "report.submit", "report", rep.ID.String(), audit.SevInfo, nil)
	return httpx.NoContent(c)
}

func (h *reportHandler) aiGrade(c *fiber.Ctx) error {
	rep, err := h.loadOwned(c)
	if err != nil {
		return err
	}
	ex, _ := h.d.store.GetExercise(c.Context(), deref(rep.ExerciseID))
	rubric := "{}"
	if ex != nil && len(ex.RubricJSON) > 0 {
		rubric = string(ex.RubricJSON)
	}
	cur, _ := auth.MustCurrent(c)
	prompt := fmt.Sprintf("Rubric JSON:\n%s\n\nStudent report (Markdown):\n%s", rubric, rep.ContentMD)
	res, err := h.d.Gateway.Chat(c.Context(), llm.Request{
		Module:    llm.ModuleReportGrading,
		Messages:  []llm.Message{{Role: "user", Content: prompt}},
		JSONMode:  true,
		MaxTokens: 800,
		UserID:    &cur.UserID,
	})
	if err != nil {
		return httpx.Unavailable("grading assistant unavailable: " + err.Error())
	}
	var parsed struct {
		Total          float64         `json:"total"`
		MaxTotal       float64         `json:"max_total"`
		OverallFeedback string         `json:"overall_feedback"`
		SuggestedGrade float64         `json:"suggested_grade_percent"`
		Criteria       json.RawMessage `json:"criteria"`
	}
	if err := json.Unmarshal([]byte(extractJSON(res.Text)), &parsed); err != nil {
		return httpx.Unavailable("grading assistant returned an unparseable response")
	}
	score := parsed.SuggestedGrade
	if score == 0 && parsed.MaxTotal > 0 {
		score = parsed.Total / parsed.MaxTotal * 100
	}
	rubricJSON, _ := json.Marshal(fiber.Map{"criteria": parsed.Criteria, "total": parsed.Total, "max_total": parsed.MaxTotal})
	_ = h.d.store.SetAIScore(c.Context(), rep.ID, score, parsed.OverallFeedback, rubricJSON)
	return httpx.OK(c, fiber.Map{
		"ai_suggested_score": score, "rationale": parsed.OverallFeedback, "criteria": parsed.Criteria,
		"note": "AI-suggested grade. Faculty score is authoritative.",
	})
}

func (h *reportHandler) grade(c *fiber.Ctx) error {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		return err
	}
	cur, _ := auth.MustCurrent(c)
	if cur.Role == auth.RoleFaculty {
		ok, err := h.d.store.FacultyOwnsReport(c.Context(), id, cur.UserID)
		if err != nil || !ok {
			return httpx.Forbidden("you do not grade this report")
		}
	}
	body, err := httpx.Bind[struct {
		Score    float64         `json:"score"`
		Feedback string          `json:"feedback"`
		Rubric   json.RawMessage `json:"rubric_json"`
	}](c)
	if err != nil {
		return err
	}
	if err := h.d.store.GradeReport(c.Context(), id, cur.UserID, body.Score, body.Feedback, body.Rubric); err != nil {
		return httpx.Internal("failed to grade report")
	}
	h.d.Audit.FromCtx(c, "report.grade", "report", id.String(), audit.SevNotice,
		map[string]any{"score": body.Score})
	return httpx.NoContent(c)
}

func (h *reportHandler) pdf(c *fiber.Ctx) error {
	rep, err := h.loadOwned(c)
	if err != nil {
		return err
	}
	meta := map[string]string{
		"Type":       rep.Type,
		"Status":     rep.Status,
		"Techniques": strings.Join(rep.TechniqueIDs, ", "),
		"Generated":  time.Now().Format("2006-01-02 15:04"),
	}
	if rep.FacultyScore != nil {
		meta["Score"] = fmt.Sprintf("%.1f / %.0f", *rep.FacultyScore, rep.MaxScore)
	}
	htmlDoc := h.renderer.HTML(rep.Title, rep.ContentMD, meta)
	data, mime, err := h.renderer.ToPDF(c.Context(), htmlDoc)
	if err != nil {
		return httpx.Internal("failed to render report: " + err.Error())
	}
	ext := "pdf"
	if mime == "text/html" {
		ext = "html"
	}
	c.Set("Content-Type", mime)
	c.Set("Content-Disposition", fmt.Sprintf(`inline; filename="report-%s.%s"`, rep.ID.String()[:8], ext))
	return c.Send(data)
}

// portfolio builds a combined PDF of a student's graded reports.
func (h *reportHandler) portfolio(c *fiber.Ctx) error {
	uid, err := httpx.ParamUUID(c, "uid")
	if err != nil {
		return err
	}
	cur, _ := auth.MustCurrent(c)
	if cur.Role == auth.RoleStudent && cur.UserID != uid {
		return httpx.Forbidden("you can only export your own portfolio")
	}
	reports, _, err := h.d.store.ListReports(c.Context(), &uid, nil, "graded", 100, 0)
	if err != nil {
		return httpx.Internal("failed to load reports")
	}
	var body strings.Builder
	body.WriteString("# Security Skills Portfolio\n\n")
	body.WriteString(fmt.Sprintf("_A verified record of hands-on red and blue team work completed in CyberRange OS._\n\n"))
	for _, r := range reports {
		body.WriteString("\n---\n\n## " + r.Title + "\n\n")
		if r.FacultyScore != nil {
			body.WriteString(fmt.Sprintf("**Score:** %.1f / %.0f\n\n", *r.FacultyScore, r.MaxScore))
		}
		if len(r.TechniqueIDs) > 0 {
			body.WriteString("**MITRE techniques:** " + strings.Join(r.TechniqueIDs, ", ") + "\n\n")
		}
		body.WriteString(r.ContentMD + "\n")
	}
	htmlDoc := h.renderer.HTML("Security Skills Portfolio", body.String(), map[string]string{"Reports": fmt.Sprintf("%d", len(reports))})
	data, mime, err := h.renderer.ToPDF(c.Context(), htmlDoc)
	if err != nil {
		return httpx.Internal("failed to render portfolio")
	}
	ext := "pdf"
	if mime == "text/html" {
		ext = "html"
	}
	c.Set("Content-Type", mime)
	c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="portfolio.%s"`, ext))
	return c.Send(data)
}

func deref(u *uuid.UUID) uuid.UUID {
	if u == nil {
		return uuid.Nil
	}
	return *u
}
