package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/cyberrange-os/api/internal/audit"
	"github.com/cyberrange-os/api/internal/auth"
	"github.com/cyberrange-os/api/internal/httpx"
	"github.com/cyberrange-os/api/internal/llm"
	"github.com/cyberrange-os/api/internal/metrics"
	"github.com/cyberrange-os/api/internal/orchestrator"
	"github.com/cyberrange-os/api/internal/realtime"
	"github.com/cyberrange-os/api/internal/store"
)

type rangeHandler struct{ d *Deps }

func newRangeHandler(d *Deps) *rangeHandler {
	h := &rangeHandler{d: d}
	go h.reaper() // background expiry of overrunning sessions
	return h
}

func (h *rangeHandler) register(r fiber.Router) {
	g := r.Group("/range-sessions")
	g.Get("/", h.list)
	g.Get("/active", h.active)
	g.Post("/", auth.RequireRole(auth.RoleStudent, auth.RoleFaculty, auth.RoleAdmin), h.start)
	g.Get("/:id", h.get)
	g.Get("/:id/commands", h.commands)
	g.Get("/:id/techniques", h.techniques)
	g.Post("/:id/end", h.end)
	g.Post("/:id/copilot/suggest", h.copilotSuggest)
	g.Post("/:id/copilot/execute", h.copilotExecute)
	g.Post("/:id/exec", h.execManual)
	g.Get("/:id/stats", h.stats)

	// Live terminal + copilot stream (token passed via ?access_token= by the browser).
	g.Get("/:id/terminal", websocket.New(h.terminalWS))
}

// ------------------------------------------------------------------ helpers

func (h *rangeHandler) loadOwnedSession(c *fiber.Ctx) (*store.RangeSession, error) {
	id, err := httpx.ParamUUID(c, "id")
	if err != nil {
		return nil, err
	}
	sess, err := h.d.store.GetSession(c.Context(), id)
	if err != nil {
		return nil, httpx.NotFound("session not found")
	}
	cur, _ := auth.MustCurrent(c)
	if cur.Role == auth.RoleStudent && sess.UserID != cur.UserID {
		return nil, httpx.Forbidden("not your session")
	}
	return sess, nil
}

// ------------------------------------------------------------------ lifecycle

func (h *rangeHandler) start(c *fiber.Ctx) error {
	if h.d.provisioner == nil {
		return httpx.Unavailable("range provisioning is unavailable (orchestrator not initialized)")
	}
	cur, _ := auth.MustCurrent(c)
	body, err := httpx.Bind[struct {
		ExerciseID string `json:"exercise_id"`
	}](c)
	if err != nil {
		return err
	}
	exerciseID, err := uuid.Parse(body.ExerciseID)
	if err != nil {
		return httpx.BadRequest("valid exercise_id is required")
	}

	// A student may only run one active session at a time.
	if existing, err := h.d.store.ActiveSessionForUser(c.Context(), cur.UserID); err == nil && existing != nil {
		return httpx.Conflict("you already have an active session; end it first")
	}

	ex, err := h.d.store.GetExercise(c.Context(), exerciseID)
	if err != nil {
		return httpx.NotFound("exercise not found")
	}
	if ex.Type != "red" {
		return httpx.BadRequest("only red-team exercises start a range session")
	}
	if cur.Role == auth.RoleStudent {
		ok, err := h.d.store.StudentCanAccessExercise(c.Context(), exerciseID, cur.UserID)
		if err != nil || !ok {
			return httpx.Forbidden("you are not enrolled in this exercise's batch")
		}
	}

	// Resolve target images from the registry ONLY (never free text).
	specs, err := h.resolveTargets(c.Context(), ex.TargetImageRefs)
	if err != nil {
		return httpx.BadRequest(err.Error())
	}
	if len(specs) == 0 {
		return httpx.BadRequest("exercise has no valid registered targets")
	}

	limit := ex.TimeLimitMinutes
	if limit <= 0 || limit > h.d.Cfg.RangeSessionMaxMin {
		limit = h.d.Cfg.RangeSessionMaxMin
	}
	expires := time.Now().Add(time.Duration(limit) * time.Minute)

	sess, err := h.d.store.CreateSession(c.Context(), exerciseID, cur.UserID, h.d.provisioner.Name(), expires)
	if err != nil {
		return httpx.Internal("failed to create session record")
	}

	h.d.Audit.FromCtx(c, "range.session.start", "range_session", sess.ID.String(), audit.SevNotice,
		map[string]any{"exercise_id": exerciseID.String(), "targets": len(specs)})

	// Provision asynchronously; the client polls GET /:id until running.
	go h.provision(sess.ID, cur.UserID, specs)

	metrics.IncGauge("active_sessions", 1)
	return httpx.Created(c, sess)
}

func (h *rangeHandler) resolveTargets(ctx context.Context, refs []string) ([]orchestrator.TargetSpec, error) {
	specs := []orchestrator.TargetSpec{}
	for i, ref := range refs {
		t, err := h.d.store.GetRangeTargetBySlug(ctx, ref)
		if err != nil {
			return nil, fmt.Errorf("target %q is not a registered range asset", ref)
		}
		if !t.IsActive {
			return nil, fmt.Errorf("target %q is deactivated", ref)
		}
		ports := make([]int, len(t.ExposedPorts))
		for j, p := range t.ExposedPorts {
			ports[j] = int(p)
		}
		host := t.Slug
		if len(refs) > 1 {
			host = fmt.Sprintf("%s-%d", t.Slug, i+1)
		}
		specs = append(specs, orchestrator.TargetSpec{
			TargetID: t.ID.String(), Slug: t.Slug, Hostname: host, Image: t.Image,
			ExposedPorts: ports, CPUQuota: t.CPUQuota, MemoryMB: int64(t.MemoryMB), Privileged: t.Privileged,
		})
	}
	return specs, nil
}

func (h *rangeHandler) provision(sessionID, userID uuid.UUID, specs []orchestrator.TargetSpec) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	rng, err := h.d.provisioner.Provision(ctx, orchestrator.ProvisionSpec{
		SessionID: sessionID.String(),
		UserID:    userID.String(),
		KaliImage: h.d.Cfg.KaliImage,
		Targets:   specs,
		CPUQuota:  h.d.Cfg.RangeCPUQuota,
		MemoryMB:  h.d.Cfg.RangeMemoryMB,
	})
	if err != nil {
		h.d.Log.Error().Err(err).Str("session", sessionID.String()).Msg("provisioning failed")
		_ = h.d.store.MarkSessionFailed(ctx, sessionID, err.Error())
		h.d.Hub.Publish(ctx, realtime.ChannelTerminal(sessionID.String()), "session.failed",
			fiber.Map{"error": err.Error()})
		metrics.IncGauge("active_sessions", -1)
		return
	}

	for _, t := range rng.Targets {
		var tid *uuid.UUID
		if id, err := uuid.Parse(t.TargetID); err == nil {
			tid = &id
		}
		_ = h.d.store.AddSessionTarget(ctx, store.SessionTarget{
			SessionID: sessionID, TargetID: tid, ContainerID: t.ContainerID,
			Hostname: t.Hostname, IPAddress: t.IPAddress, Image: t.Image, Status: t.Status,
		})
	}
	terminalToken := auth.RandomSecret(24)
	if err := h.d.store.MarkSessionRunning(ctx, sessionID, rng.NetworkID, rng.NetworkName, rng.AttackerID, rng.AttackerName, terminalToken); err != nil {
		h.d.Log.Error().Err(err).Msg("failed to mark session running")
	}
	h.d.Hub.Publish(ctx, realtime.ChannelTerminal(sessionID.String()), "session.running",
		fiber.Map{"targets": rng.Targets, "attacker_ip": rng.AttackerIP})
	h.d.Log.Info().Str("session", sessionID.String()).Msg("range session running")
}

func (h *rangeHandler) get(c *fiber.Ctx) error {
	sess, err := h.loadOwnedSession(c)
	if err != nil {
		return err
	}
	used, cap := h.d.Gateway.BudgetUsed(c.Context(), sess.ID)
	return httpx.OK(c, fiber.Map{"session": sess, "llm_budget_used": used, "llm_budget_cap": cap})
}

func (h *rangeHandler) list(c *fiber.Ctx) error {
	cur, _ := auth.MustCurrent(c)
	page := httpx.ParsePage(c, 20, 100)
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
	items, total, err := h.d.store.ListSessions(c.Context(), userFilter, exFilter, page.Limit, page.Offset)
	if err != nil {
		return httpx.Internal("failed to list sessions")
	}
	return httpx.OK(c, httpx.ListResponse[store.RangeSession]{Items: items, Total: total})
}

func (h *rangeHandler) active(c *fiber.Ctx) error {
	cur, _ := auth.MustCurrent(c)
	sess, err := h.d.store.ActiveSessionForUser(c.Context(), cur.UserID)
	if err != nil {
		return httpx.OK(c, fiber.Map{"session": nil})
	}
	return httpx.OK(c, fiber.Map{"session": sess})
}

func (h *rangeHandler) commands(c *fiber.Ctx) error {
	sess, err := h.loadOwnedSession(c)
	if err != nil {
		return err
	}
	log, err := h.d.store.CommandLog(c.Context(), sess.ID)
	if err != nil {
		return httpx.Internal("failed to load command log")
	}
	return httpx.OK(c, httpx.ListResponse[store.CommandLogEntry]{Items: log, Total: len(log)})
}

func (h *rangeHandler) techniques(c *fiber.Ctx) error {
	sess, err := h.loadOwnedSession(c)
	if err != nil {
		return err
	}
	demonstrated, _ := h.d.store.SessionTechniques(c.Context(), sess.ID)
	ex, _ := h.d.store.GetExercise(c.Context(), sess.ExerciseID)
	expected := []string{}
	if ex != nil {
		expected = ex.ExpectedTechniques
	}
	return httpx.OK(c, fiber.Map{"expected": expected, "demonstrated": demonstrated})
}

// stats returns live CPU/RAM usage for the session's containers.
func (h *rangeHandler) stats(c *fiber.Ctx) error {
	sess, err := h.loadOwnedSession(c)
	if err != nil {
		return err
	}
	if h.d.provisioner == nil || sess.Status != "running" {
		return httpx.OK(c, fiber.Map{"stats": map[string]any{}})
	}
	ctx, cancel := context.WithTimeout(c.Context(), 8*time.Second)
	defer cancel()
	stats, err := h.d.provisioner.Stats(ctx, sessionToRange(sess))
	if err != nil {
		return httpx.OK(c, fiber.Map{"stats": map[string]any{}, "error": err.Error()})
	}
	return httpx.OK(c, fiber.Map{"stats": stats})
}

func (h *rangeHandler) end(c *fiber.Ctx) error {
	sess, err := h.loadOwnedSession(c)
	if err != nil {
		return err
	}
	if sess.Status == "completed" || sess.Status == "failed" || sess.Status == "expired" {
		return httpx.OK(c, sess)
	}
	h.teardown(c.Context(), sess, "completed")
	h.d.Audit.FromCtx(c, "range.session.end", "range_session", sess.ID.String(), audit.SevInfo, nil)
	final, _ := h.d.store.GetSession(c.Context(), sess.ID)
	return httpx.OK(c, final)
}

func (h *rangeHandler) teardown(ctx context.Context, sess *store.RangeSession, status string) {
	rng := sessionToRange(sess)
	if h.d.provisioner != nil {
		if err := h.d.provisioner.Teardown(ctx, rng); err != nil {
			h.d.Log.Warn().Err(err).Str("session", sess.ID.String()).Msg("teardown reported error")
		}
	}
	ratio := 0.0
	if sess.TotalActions > 0 {
		ratio = float64(sess.AIActions) / float64(sess.TotalActions)
	}
	xp := h.computeXP(ctx, sess, ratio)
	_ = h.d.store.MarkSessionEnded(ctx, sess.ID, status, ratio, xp)
	h.awardXP(ctx, sess, xp)
	metrics.IncGauge("active_sessions", -1)
	h.d.Hub.Publish(ctx, realtime.ChannelTerminal(sess.ID.String()), "session.ended",
		fiber.Map{"status": status, "xp": xp, "assistance_ratio": ratio})
}

func sessionToRange(sess *store.RangeSession) *orchestrator.Range {
	rng := &orchestrator.Range{
		NetworkID: sess.NetworkID, NetworkName: sess.NetworkName,
		AttackerID: sess.AttackerID, AttackerName: sess.AttackerName, Driver: sess.Driver,
	}
	for _, t := range sess.Targets {
		rng.Targets = append(rng.Targets, orchestrator.ProvisionedTarget{
			TargetID: uuidOrEmpty(t.TargetID), Hostname: t.Hostname, ContainerID: t.ContainerID, IPAddress: t.IPAddress,
		})
	}
	return rng
}

func uuidOrEmpty(u *uuid.UUID) string {
	if u == nil {
		return ""
	}
	return u.String()
}

// ------------------------------------------------------------------ copilot

func (h *rangeHandler) copilotSuggest(c *fiber.Ctx) error {
	sess, err := h.loadOwnedSession(c)
	if err != nil {
		return err
	}
	if sess.Status != "running" {
		return httpx.BadRequest("session is not running")
	}
	cur, _ := auth.MustCurrent(c)
	body, _ := httpx.Bind[struct {
		Question string `json:"question"`
	}](c)

	ex, _ := h.d.store.GetExercise(c.Context(), sess.ExerciseID)
	ctxText := h.buildCopilotContext(c.Context(), sess, ex, body.Question)

	res, err := h.d.Gateway.Chat(c.Context(), llm.Request{
		Module:    llm.ModulePentestCopilot,
		Messages:  []llm.Message{{Role: "user", Content: ctxText}},
		JSONMode:  true,
		MaxTokens: 500,
		UserID:    &cur.UserID,
		SessionID: &sess.ID,
	})
	if err != nil {
		return httpx.Unavailable("copilot unavailable: " + err.Error())
	}

	var parsed struct {
		Rationale        string `json:"rationale"`
		Command          string `json:"command"`
		Tool             string `json:"tool"`
		MitreTechniqueID string `json:"mitre_technique_id"`
		ExpectedOutcome  string `json:"expected_outcome"`
	}
	if err := json.Unmarshal([]byte(extractJSON(res.Text)), &parsed); err != nil || parsed.Command == "" {
		return httpx.Unavailable("copilot returned an unparseable suggestion")
	}

	sug, err := h.d.store.InsertSuggestion(c.Context(), store.Suggestion{
		SessionID: sess.ID, Command: parsed.Command, Rationale: parsed.Rationale,
		MitreTechniqueID: strings.ToUpper(parsed.MitreTechniqueID), Tool: parsed.Tool,
		PromptVersion: res.PromptVersion, Model: res.Model,
	})
	if err != nil {
		return httpx.Internal("failed to persist suggestion")
	}
	return httpx.OK(c, fiber.Map{
		"suggestion":       sug,
		"expected_outcome": parsed.ExpectedOutcome,
		"note":             "AI-suggested. Review before you Approve & Run.",
	})
}

func (h *rangeHandler) buildCopilotContext(ctx context.Context, sess *store.RangeSession, ex *store.Exercise, question string) string {
	var sb strings.Builder
	if ex != nil {
		sb.WriteString("## Scenario brief\n")
		sb.WriteString(ex.BriefMD)
		sb.WriteString("\n\n")
		if len(ex.ExpectedTechniques) > 0 {
			sb.WriteString("Expected techniques: " + strings.Join(ex.ExpectedTechniques, ", ") + "\n\n")
		}
	}
	sb.WriteString("## Targets in range\n")
	for _, t := range sess.Targets {
		sb.WriteString(fmt.Sprintf("- %s (%s) image=%s\n", t.Hostname, t.IPAddress, t.Image))
	}
	sb.WriteString("\n## Recent commands and output (most recent last)\n")
	log, _ := h.d.store.CommandLog(ctx, sess.ID)
	start := 0
	if len(log) > 6 {
		start = len(log) - 6
	}
	for _, e := range log[start:] {
		out := e.Output
		if len(out) > 1500 {
			out = out[len(out)-1500:]
		}
		sb.WriteString(fmt.Sprintf("$ %s\n%s\n\n", e.Command, out))
	}
	if strings.TrimSpace(question) != "" {
		sb.WriteString("## Student question\n" + question + "\n")
	}
	sb.WriteString("\nPropose the single best next action as JSON.")
	return sb.String()
}

func (h *rangeHandler) copilotExecute(c *fiber.Ctx) error {
	sess, err := h.loadOwnedSession(c)
	if err != nil {
		return err
	}
	body, err := httpx.Bind[struct {
		SuggestionID string `json:"suggestion_id"`
		Command      string `json:"command"` // optional student-modified command
	}](c)
	if err != nil {
		return err
	}
	sugID, err := uuid.Parse(body.SuggestionID)
	if err != nil {
		return httpx.BadRequest("valid suggestion_id required")
	}
	sug, err := h.d.store.GetSuggestion(c.Context(), sugID)
	if err != nil || sug.SessionID != sess.ID {
		return httpx.NotFound("suggestion not found")
	}

	command := sug.Command
	modified := false
	if strings.TrimSpace(body.Command) != "" && strings.TrimSpace(body.Command) != strings.TrimSpace(sug.Command) {
		command = body.Command
		modified = true
	}
	status := "approved"
	if modified {
		status = "modified"
	}
	_ = h.d.store.SetSuggestionStatus(c.Context(), sugID, status)

	return h.runAndLog(c, sess, command, true, sug.Rationale, sug.MitreTechniqueID, modified)
}

// execManual runs a command the student typed directly (still logged as
// evidence, still AI-unassisted).
func (h *rangeHandler) execManual(c *fiber.Ctx) error {
	sess, err := h.loadOwnedSession(c)
	if err != nil {
		return err
	}
	body, err := httpx.Bind[struct {
		Command string `json:"command"`
	}](c)
	if err != nil {
		return err
	}
	if strings.TrimSpace(body.Command) == "" {
		return httpx.BadRequest("command is required")
	}
	return h.runAndLog(c, sess, body.Command, false, "", "", false)
}

func (h *rangeHandler) runAndLog(c *fiber.Ctx, sess *store.RangeSession, command string, aiSuggested bool, rationale, technique string, modified bool) error {
	if sess.Status != "running" {
		return httpx.BadRequest("session is not running")
	}
	if h.d.provisioner == nil {
		return httpx.Unavailable("orchestrator unavailable")
	}
	cur, _ := auth.MustCurrent(c)

	execCtx, cancel := context.WithTimeout(c.Context(), 4*time.Minute)
	defer cancel()

	start := time.Now()
	result, err := h.d.provisioner.Exec(execCtx, sess.AttackerID, command)
	dur := int(time.Since(start).Milliseconds())
	if err != nil {
		return httpx.Unavailable("command execution failed: " + err.Error())
	}

	output := result.Stdout
	if result.Stderr != "" {
		output += "\n" + result.Stderr
	}
	exit := result.ExitCode

	// Auto-tag with MITRE if the copilot didn't already provide a technique.
	if technique == "" {
		if tech, conf, terr := h.d.mitreEngine.Tag(execCtx, command); terr == nil && conf >= 0.4 {
			technique = tech
		}
	}
	var techPtr *string
	if technique != "" {
		techPtr = &technique
	}

	seq, _ := h.d.store.NextCommandSeq(c.Context(), sess.ID)
	entry, err := h.d.store.InsertCommandLog(c.Context(), store.CommandLogInput{
		SessionID: sess.ID, Seq: seq, Command: command, Output: output, ExitCode: &exit,
		Technique: techPtr, WasAISuggested: aiSuggested, AIRationale: rationale,
		WasModified: modified, DurationMS: dur,
	})
	if err != nil {
		return httpx.Internal("failed to log command")
	}

	h.d.Audit.Write(c.Context(), audit.Entry{
		ActorID: &cur.UserID, ActorRole: string(cur.Role), Action: "range.command.execute",
		TargetType: "range_session", TargetID: sess.ID.String(), Severity: audit.SevInfo, IP: c.IP(),
		Metadata: map[string]any{"command": command, "ai_suggested": aiSuggested, "technique": technique},
	})

	h.d.Hub.Publish(c.Context(), realtime.ChannelTerminal(sess.ID.String()), "command.result", entry)
	return httpx.OK(c, entry)
}

// ------------------------------------------------------------------ terminal WS

func (h *rangeHandler) terminalWS(conn *websocket.Conn) {
	sessionID := conn.Params("id")
	ctx := context.Background()
	ch, cleanup := h.d.Hub.Subscribe(ctx, realtime.ChannelTerminal(sessionID))
	defer cleanup()
	defer conn.Close()

	// Reader goroutine detects client disconnect.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	_ = conn.WriteJSON(fiber.Map{"type": "connected", "session_id": sessionID})
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

// ------------------------------------------------------------------ reaper

func (h *rangeHandler) reaper() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		expired, err := h.d.store.ExpiredSessions(ctx)
		if err == nil {
			for i := range expired {
				sess, _ := h.d.store.GetSession(ctx, expired[i].ID)
				if sess != nil {
					h.d.Log.Info().Str("session", sess.ID.String()).Msg("reaping expired session")
					h.teardown(ctx, sess, "expired")
				}
			}
		}
		cancel()
	}
}
