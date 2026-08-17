package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/cyberrange-os/api/internal/audit"
	"github.com/cyberrange-os/api/internal/auth"
	"github.com/cyberrange-os/api/internal/httpx"
	"github.com/cyberrange-os/api/internal/llm"
	"github.com/cyberrange-os/api/internal/realtime"
	"github.com/cyberrange-os/api/internal/store"
)

type siemHandler struct{ d *Deps }

func newSIEMHandler(d *Deps) *siemHandler { return &siemHandler{d: d} }

func (h *siemHandler) register(r fiber.Router) {
	g := r.Group("/siem")
	g.Get("/alerts", h.listAlerts)
	g.Get("/alerts/:id", h.getAlert)
	g.Post("/alerts/:id/detect", h.markDetected)
	g.Post("/alerts/:id/copilot/summarize", h.summarize)
	g.Post("/alerts/:id/resolve", h.resolve)
	g.Post("/alerts/:id/ground-truth", auth.RequireRole(auth.RoleFaculty, auth.RoleAdmin), h.setGroundTruth)
	g.Get("/accuracy", auth.RequireRole(auth.RoleFaculty, auth.RoleAdmin), h.accuracy)
	g.Get("/metrics", h.metrics)
	g.Get("/alerts/live", websocket.New(h.liveWS))

	// Playbooks (blue team)
	g.Get("/playbooks", h.listPlaybooks)
	g.Post("/playbooks", auth.RequireRole(auth.RoleFaculty, auth.RoleAdmin), h.createPlaybook)
	g.Post("/playbooks/:id/progress", h.setPlaybookProgress)
}

func (h *siemHandler) listAlerts(c *fiber.Ctx) error {
	page := httpx.ParsePage(c, 50, 200)
	f := store.AlertFilter{Severity: c.Query("severity"), Unresolved: c.Query("unresolved") == "true", Limit: page.Limit, Offset: page.Offset}
	if sid, ok := httpx.QueryUUID(c, "session_id"); ok {
		f.SessionID = &sid
	}
	if eid, ok := httpx.QueryUUID(c, "exercise_id"); ok {
		f.ExerciseID = &eid
	}
	alerts, total, err := h.d.store.ListAlerts(c.Context(), f)
	if err != nil {
		return httpx.Internal("failed to list alerts")
	}
	return httpx.OK(c, httpx.ListResponse[store.Alert]{Items: alerts, Total: total})
}

func (h *siemHandler) getAlert(c *fiber.Ctx) error {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		return err
	}
	alert, err := h.d.store.GetAlert(c.Context(), id)
	if err != nil {
		return httpx.NotFound("alert not found")
	}
	return httpx.OK(c, alert)
}

// markDetected stamps the first-view time so MTTD can be measured.
func (h *siemHandler) markDetected(c *fiber.Ctx) error {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		return err
	}
	if err := h.d.store.MarkDetected(c.Context(), id); err != nil {
		return httpx.Internal("failed to mark detected")
	}
	return httpx.NoContent(c)
}

func (h *siemHandler) summarize(c *fiber.Ctx) error {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		return err
	}
	alert, err := h.d.store.GetAlert(c.Context(), id)
	if err != nil {
		return httpx.NotFound("alert not found")
	}
	cur, _ := auth.MustCurrent(c)

	// Build context: this alert + recent alerts from the same source IP.
	recent, _ := h.d.store.RecentFromSource(c.Context(), alert.SrcIP, alert.ID, 5)
	prompt := buildAlertPrompt(alert, recent)

	res, err := h.d.Gateway.Chat(c.Context(), llm.Request{
		Module:    llm.ModuleSOCCopilot,
		Messages:  []llm.Message{{Role: "user", Content: prompt}},
		JSONMode:  true,
		MaxTokens: 600,
		UserID:    &cur.UserID,
	})
	if err != nil {
		return httpx.Unavailable("SOC copilot unavailable: " + err.Error())
	}
	var parsed struct {
		Summary           string  `json:"summary"`
		Verdict           string  `json:"verdict"`
		Confidence        float64 `json:"confidence"`
		Reasoning         string  `json:"reasoning"`
		MitreTechniqueID  string  `json:"mitre_technique_id"`
		NextStep          string  `json:"next_step"`
		IncidentParagraph string  `json:"incident_paragraph"`
	}
	if err := json.Unmarshal([]byte(extractJSON(res.Text)), &parsed); err != nil {
		return httpx.Unavailable("SOC copilot returned an unparseable response")
	}
	if parsed.Verdict != "" {
		_ = h.d.store.SetAISuggestion(c.Context(), alert.ID, normalizeLabel(parsed.Verdict), parsed.Confidence)
	}
	return httpx.OK(c, fiber.Map{
		"summary": parsed.Summary, "verdict": parsed.Verdict, "confidence": parsed.Confidence,
		"reasoning": parsed.Reasoning, "mitre_technique_id": parsed.MitreTechniqueID,
		"next_step": parsed.NextStep, "incident_paragraph": parsed.IncidentParagraph,
		"note": "AI-suggested, verify before submitting.",
	})
}

func buildAlertPrompt(a *store.Alert, recent []store.Alert) string {
	var sb strings.Builder
	sb.WriteString("## Alert under triage\n")
	sb.WriteString(fmt.Sprintf("Source system: %s\nRule: %s (%s)\nSeverity: %s\nSource IP: %s -> Dest IP: %s\n",
		a.Source, a.RuleID, a.RuleDescription, a.Severity, a.SrcIP, a.DstIP))
	sb.WriteString("Raw log:\n")
	sb.WriteString(string(a.RawLog))
	sb.WriteString("\n\n## Recent alerts from the same source IP\n")
	if len(recent) == 0 {
		sb.WriteString("(none)\n")
	}
	for _, r := range recent {
		sb.WriteString(fmt.Sprintf("- [%s] %s (%s)\n", r.Severity, r.RuleDescription, r.EventAt.Format("15:04:05")))
	}
	sb.WriteString("\nTriage this alert and respond as JSON.")
	return sb.String()
}

func (h *siemHandler) resolve(c *fiber.Ctx) error {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		return err
	}
	cur, _ := auth.MustCurrent(c)
	body, err := httpx.Bind[struct {
		StudentLabel string `json:"student_label"`
		Note         string `json:"resolution_note"`
	}](c)
	if err != nil {
		return err
	}
	if strings.TrimSpace(body.Note) == "" {
		return httpx.BadRequest("a written resolution note is required")
	}
	if err := h.d.store.ResolveAlert(c.Context(), id, cur.UserID, normalizeLabel(body.StudentLabel), body.Note); err != nil {
		return httpx.Internal("failed to resolve alert")
	}
	h.d.Audit.FromCtx(c, "siem.alert.resolve", "siem_alert", id.String(), audit.SevInfo,
		map[string]any{"label": body.StudentLabel})
	// Award blue-team XP for triage.
	alert, _ := h.d.store.GetAlert(c.Context(), id)
	if alert != nil && alert.ExerciseID != nil {
		if batchID, err := h.d.store.BatchForExercise(c.Context(), *alert.ExerciseID); err == nil {
			_ = h.d.store.AddXP(c.Context(), cur.UserID, batchID, "blue", 25, "alert triaged", "siem_alert", &id)
			_ = h.d.store.RecomputeLeaderboard(c.Context(), batchID)
		}
	}
	h.d.Hub.Publish(c.Context(), realtime.ChannelAlerts("all"), "alert.resolved", fiber.Map{"id": id})
	return httpx.NoContent(c)
}

func (h *siemHandler) setGroundTruth(c *fiber.Ctx) error {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		return err
	}
	body, err := httpx.Bind[struct {
		Label string `json:"label"`
	}](c)
	if err != nil {
		return err
	}
	if err := h.d.store.SetGroundTruth(c.Context(), id, normalizeLabel(body.Label)); err != nil {
		return httpx.Internal("failed to set ground truth")
	}
	h.d.Audit.FromCtx(c, "siem.alert.ground_truth", "siem_alert", id.String(), audit.SevNotice, nil)
	return httpx.NoContent(c)
}

func (h *siemHandler) accuracy(c *fiber.Ctx) error {
	var exID *uuid.UUID
	if eid, ok := httpx.QueryUUID(c, "exercise_id"); ok {
		exID = &eid
	}
	acc, err := h.d.store.SOCAccuracy(c.Context(), exID)
	if err != nil {
		return httpx.Internal("failed to compute accuracy")
	}
	return httpx.OK(c, acc)
}

func (h *siemHandler) metrics(c *fiber.Ctx) error {
	var exID *uuid.UUID
	if eid, ok := httpx.QueryUUID(c, "exercise_id"); ok {
		exID = &eid
	}
	m, err := h.d.store.ResponseMetrics(c.Context(), exID)
	if err != nil {
		return httpx.Internal("failed to compute metrics")
	}
	return httpx.OK(c, m)
}

func (h *siemHandler) liveWS(conn *websocket.Conn) {
	ctx := context.Background()
	ch, cleanup := h.d.Hub.Subscribe(ctx, realtime.ChannelAlerts("all"))
	defer cleanup()
	defer conn.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
	_ = conn.WriteJSON(fiber.Map{"type": "connected"})
	for {
		select {
		case <-done:
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if err := conn.WriteJSON(ev); err != nil {
				return
			}
		}
	}
}

// ------------------------------------------------------------------ playbooks

func (h *siemHandler) listPlaybooks(c *fiber.Ctx) error {
	eid, ok := httpx.QueryUUID(c, "exercise_id")
	if !ok {
		return httpx.BadRequest("exercise_id is required")
	}
	pbs, err := h.d.store.PlaybooksForExercise(c.Context(), eid)
	if err != nil {
		return httpx.Internal("failed to list playbooks")
	}
	return httpx.OK(c, httpx.ListResponse[store.Playbook]{Items: pbs, Total: len(pbs)})
}

func (h *siemHandler) createPlaybook(c *fiber.Ctx) error {
	cur, _ := auth.MustCurrent(c)
	body, err := httpx.Bind[struct {
		ExerciseID string          `json:"exercise_id"`
		Title      string          `json:"title"`
		ContentMD  string          `json:"content_md"`
		StepsJSON  json.RawMessage `json:"steps_json"`
	}](c)
	if err != nil {
		return err
	}
	var exID *uuid.UUID
	if body.ExerciseID != "" {
		if id, err := uuid.Parse(body.ExerciseID); err == nil {
			exID = &id
		}
	}
	pb, err := h.d.store.CreatePlaybook(c.Context(), exID, body.Title, body.ContentMD, body.StepsJSON, &cur.UserID)
	if err != nil {
		return httpx.Internal("failed to create playbook")
	}
	return httpx.Created(c, pb)
}

func (h *siemHandler) setPlaybookProgress(c *fiber.Ctx) error {
	pbID, err := httpx.ParamUUID(c, "id")
	if err != nil {
		return err
	}
	body, err := httpx.Bind[struct {
		SessionID string `json:"session_id"`
		StepIndex int    `json:"step_index"`
		Done      bool   `json:"done"`
		Note      string `json:"note"`
	}](c)
	if err != nil {
		return err
	}
	sid, err := uuid.Parse(body.SessionID)
	if err != nil {
		return httpx.BadRequest("valid session_id required")
	}
	if err := h.d.store.SetPlaybookProgress(c.Context(), pbID, sid, body.StepIndex, body.Done, body.Note); err != nil {
		return httpx.Internal("failed to update progress")
	}
	return httpx.NoContent(c)
}

func normalizeLabel(l string) string {
	switch strings.ToLower(strings.TrimSpace(l)) {
	case "true_positive", "tp", "true positive":
		return "true_positive"
	case "false_positive", "fp", "false positive":
		return "false_positive"
	case "benign", "b":
		return "benign"
	}
	return ""
}
