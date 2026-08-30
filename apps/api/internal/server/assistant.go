package server

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/cyberrange-os/api/internal/audit"
	"github.com/cyberrange-os/api/internal/auth"
	"github.com/cyberrange-os/api/internal/httpx"
	"github.com/cyberrange-os/api/internal/llm"
)

// The Department Assistant answers routine/how-to questions for students and
// faculty using the local model, grounded in an admin-editable knowledge base.
// It powers the real-time voice assistant in the web app (speech is handled
// browser-side; the answers come from the local LLM so nothing leaves the lab).
type assistantHandler struct{ d *Deps }

func newAssistantHandler(d *Deps) *assistantHandler { return &assistantHandler{d: d} }

const kbSettingKey = "assistant_knowledge"

const defaultKnowledge = `# Department Knowledge Base

_Edit this from the Assistant page (faculty/admin) to teach the assistant your
department's real routine. Everything here is used to answer student questions._

## Lab timings
- Cybersecurity Lab is open Monday to Friday, 9:00 AM to 5:00 PM.
- Supervised range sessions run during scheduled practical hours only.

## Getting help
- For account or enrollment issues, contact the department office.
- For exercise or grading questions, ask your assigned faculty.

## Platform basics
- Log in with your college email or roll number.
- Red Team and Blue Team consoles are available during scheduled sessions.
- Submit reports from the Reports section; graded scores appear there.
`

func (h *assistantHandler) register(r fiber.Router) {
	g := r.Group("/assistant")
	g.Post("/ask", h.ask)
	g.Get("/knowledge", h.getKnowledge)
	g.Put("/knowledge", auth.RequireRole(auth.RoleFaculty, auth.RoleAdmin), h.setKnowledge)
}

func (h *assistantHandler) knowledge(ctx context.Context) string {
	raw, err := h.d.store.GetSetting(ctx, kbSettingKey)
	if err != nil || len(raw) == 0 {
		return defaultKnowledge
	}
	var payload struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil || strings.TrimSpace(payload.Content) == "" {
		return defaultKnowledge
	}
	return payload.Content
}

type askRequest struct {
	Question string        `json:"question"`
	History  []chatTurn    `json:"history"`
}

type chatTurn struct {
	Role    string `json:"role"`    // "user" | "assistant"
	Content string `json:"content"`
}

func (h *assistantHandler) ask(c *fiber.Ctx) error {
	cur, _ := auth.MustCurrent(c)
	body, err := httpx.Bind[askRequest](c)
	if err != nil {
		return err
	}
	if strings.TrimSpace(body.Question) == "" {
		return httpx.BadRequest("question is required")
	}

	kb := h.knowledge(c.Context())

	msgs := []llm.Message{
		{Role: "system", Content: "KNOWLEDGE BASE:\n" + kb},
	}
	// Keep a short rolling window of prior turns for conversational context.
	start := 0
	if len(body.History) > 6 {
		start = len(body.History) - 6
	}
	for _, t := range body.History[start:] {
		role := "user"
		if t.Role == "assistant" {
			role = "assistant"
		}
		if strings.TrimSpace(t.Content) != "" {
			msgs = append(msgs, llm.Message{Role: role, Content: t.Content})
		}
	}
	msgs = append(msgs, llm.Message{Role: "user", Content: body.Question})

	res, err := h.d.Gateway.Chat(c.Context(), llm.Request{
		Module:      llm.ModuleAssistant,
		Messages:    msgs,
		Temperature: 0.3,
		MaxTokens:   400,
		UserID:      &cur.UserID,
	})
	if err != nil {
		return httpx.Unavailable("assistant unavailable: " + err.Error())
	}
	return httpx.OK(c, fiber.Map{"answer": res.Text, "model": res.Model})
}

func (h *assistantHandler) getKnowledge(c *fiber.Ctx) error {
	return httpx.OK(c, fiber.Map{"content": h.knowledge(c.Context())})
}

func (h *assistantHandler) setKnowledge(c *fiber.Ctx) error {
	cur, _ := auth.MustCurrent(c)
	body, err := httpx.Bind[struct {
		Content string `json:"content"`
	}](c)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(fiber.Map{"content": body.Content})
	if err := h.d.store.SetSetting(c.Context(), kbSettingKey, payload, &cur.UserID); err != nil {
		return httpx.Internal("failed to save knowledge base")
	}
	h.d.Audit.FromCtx(c, "assistant.knowledge.update", "platform_setting", kbSettingKey, audit.SevNotice, nil)
	return httpx.NoContent(c)
}
