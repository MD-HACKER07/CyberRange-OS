package llm

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/cyberrange-os/api/internal/config"
)

// Module names double as the key for prompt versions and model approvals.
const (
	ModulePentestCopilot = "pentest-copilot"
	ModuleSOCCopilot     = "soc-copilot"
	ModuleReportGrading  = "report-grading"
	ModuleMITRETagging   = "mitre-tagging"
	ModuleRedTeamTarget  = "red-teaming-target"
	ModuleAssistant      = "assistant"
)

var Modules = []string{
	ModulePentestCopilot, ModuleSOCCopilot, ModuleReportGrading,
	ModuleMITRETagging, ModuleRedTeamTarget, ModuleAssistant,
}

var ErrBudgetExceeded = errors.New("session token budget exceeded")

type ModelBinding struct {
	Name     string  `json:"name"`
	Endpoint string  `json:"endpoint"`
	Runtime  Runtime `json:"runtime"`
	Context  int     `json:"context_window"`
}

type Request struct {
	Module      string
	Model       string // optional override, must be registered for the module
	System      string // optional override; default is the active DB prompt
	Messages    []Message
	Temperature float64
	MaxTokens   int
	JSONMode    bool
	UserID      *uuid.UUID
	SessionID   *uuid.UUID
}

type Result struct {
	Text          string  `json:"text"`
	Model         string  `json:"model"`
	Endpoint      string  `json:"endpoint"`
	PromptVersion int     `json:"prompt_version"`
	Tokens        int     `json:"tokens"`
	LatencyMS     int     `json:"latency_ms"`
	Module        string  `json:"module"`
	CallID        string  `json:"call_id"`
	Temperature   float64 `json:"temperature"`
}

type Gateway struct {
	cfg    *config.Config
	pool   *pgxpool.Pool
	rdb    *redis.Client
	client *Client
	log    zerolog.Logger

	assertion *EgressAssertion
}

// New runs the local-inference guard, then makes sure every module has an
// active system prompt version in the database.
func New(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool, rdb *redis.Client, log zerolog.Logger) (*Gateway, error) {
	assertion, err := AssertLocalEndpoint(cfg.LLMBaseURL, cfg.LLMAllowPublicIP)
	if err != nil {
		return nil, err
	}
	if !assertion.AllPrivate {
		log.Warn().Str("endpoint", cfg.LLMBaseURL).Msg(assertion.Message)
	} else {
		log.Info().Str("endpoint", cfg.LLMBaseURL).Strs("ips", assertion.ResolvedIPs).
			Msg("local inference endpoint verified as private")
	}

	g := &Gateway{
		cfg:       cfg,
		pool:      pool,
		rdb:       rdb,
		client:    NewClient(cfg.LLMRequestTimeout),
		log:       log,
		assertion: assertion,
	}
	if err := g.EnsureDefaultPrompts(ctx); err != nil {
		return nil, err
	}
	if err := g.ensureDefaultModel(ctx); err != nil {
		return nil, err
	}
	return g, nil
}

func (g *Gateway) Assertion() *EgressAssertion { return g.assertion }

// RefreshAssertion re-resolves the endpoint (Admin panel "verify now" action).
func (g *Gateway) RefreshAssertion() *EgressAssertion {
	if a, err := AssertLocalEndpoint(g.cfg.LLMBaseURL, g.cfg.LLMAllowPublicIP); a != nil {
		g.assertion = a
		if err != nil {
			g.log.Error().Err(err).Msg("egress assertion failed")
		}
	}
	return g.assertion
}

// ------------------------------------------------------------ model binding

func (g *Gateway) ensureDefaultModel(ctx context.Context) error {
	var n int
	if err := g.pool.QueryRow(ctx, `SELECT count(*) FROM llm_models`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	_, err := g.pool.Exec(ctx, `
		INSERT INTO llm_models (name, endpoint, runtime, context_window, modules, is_default, is_active, notes)
		VALUES ($1,$2,$3,$4,$5,TRUE,TRUE,$6)
		ON CONFLICT (name, endpoint) DO NOTHING`,
		g.cfg.LLMDefaultModel, g.cfg.LLMBaseURL, "ollama", 8192, Modules,
		"Auto-registered from LLM_BASE_URL / LLM_DEFAULT_MODEL at first boot.")
	return err
}

func (g *Gateway) ModelFor(ctx context.Context, module, override string) (ModelBinding, error) {
	var b ModelBinding
	var runtime string
	q := `SELECT name, endpoint, runtime, context_window FROM llm_models
	      WHERE is_active AND $1 = ANY(modules) AND ($2='' OR name=$2)
	      ORDER BY is_default DESC, created_at ASC LIMIT 1`
	err := g.pool.QueryRow(ctx, q, module, override).Scan(&b.Name, &b.Endpoint, &runtime, &b.Context)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			if override != "" {
				return b, fmt.Errorf("model %q is not registered/approved for module %s", override, module)
			}
			// Fall back to the configured endpoint so the platform still works
			// before an admin curates the registry.
			return ModelBinding{
				Name: g.cfg.LLMDefaultModel, Endpoint: g.cfg.LLMBaseURL,
				Runtime: RuntimeOllama, Context: 8192,
			}, nil
		}
		return b, err
	}
	b.Runtime = Runtime(runtime)
	return b, nil
}

// ------------------------------------------------------------ prompt registry

func (g *Gateway) ActivePrompt(ctx context.Context, module string) (int, string, error) {
	var version int
	var prompt string
	err := g.pool.QueryRow(ctx,
		`SELECT version, system_prompt FROM llm_prompts WHERE module=$1 AND active LIMIT 1`, module).
		Scan(&version, &prompt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			if def, ok := DefaultPrompts[module]; ok {
				return 0, def, nil
			}
			return 0, "", fmt.Errorf("no active prompt for module %s", module)
		}
		return 0, "", err
	}
	return version, prompt, nil
}

func (g *Gateway) EnsureDefaultPrompts(ctx context.Context) error {
	for _, module := range Modules {
		var n int
		if err := g.pool.QueryRow(ctx, `SELECT count(*) FROM llm_prompts WHERE module=$1`, module).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		if _, err := g.pool.Exec(ctx, `
			INSERT INTO llm_prompts (module, version, system_prompt, active, notes)
			VALUES ($1, 1, $2, TRUE, 'Seeded default prompt (v1)')
			ON CONFLICT (module, version) DO NOTHING`, module, DefaultPrompts[module]); err != nil {
			return err
		}
	}
	return nil
}

// ------------------------------------------------------------------ budgets

func (g *Gateway) budgetKey(sessionID uuid.UUID) string { return "llm:budget:session:" + sessionID.String() }

func (g *Gateway) CheckBudget(ctx context.Context, sessionID *uuid.UUID) error {
	if sessionID == nil || g.cfg.LLMSessionTokenCap <= 0 {
		return nil
	}
	val, err := g.rdb.Get(ctx, g.budgetKey(*sessionID)).Result()
	if err != nil {
		return nil // budget tracking is best-effort; Redis outage must not block teaching
	}
	used, _ := strconv.Atoi(val)
	if used >= g.cfg.LLMSessionTokenCap {
		return ErrBudgetExceeded
	}
	return nil
}

func (g *Gateway) addBudget(ctx context.Context, sessionID *uuid.UUID, tokens int) {
	if sessionID == nil || tokens <= 0 {
		return
	}
	key := g.budgetKey(*sessionID)
	if err := g.rdb.IncrBy(ctx, key, int64(tokens)).Err(); err == nil {
		g.rdb.Expire(ctx, key, 24*time.Hour)
	}
	_, _ = g.pool.Exec(ctx, `UPDATE range_sessions SET llm_tokens_used = llm_tokens_used + $2 WHERE id=$1`, *sessionID, tokens)
}

// BudgetUsed reports consumption for the session HUD.
func (g *Gateway) BudgetUsed(ctx context.Context, sessionID uuid.UUID) (int, int) {
	val, err := g.rdb.Get(ctx, g.budgetKey(sessionID)).Result()
	if err != nil {
		return 0, g.cfg.LLMSessionTokenCap
	}
	used, _ := strconv.Atoi(val)
	return used, g.cfg.LLMSessionTokenCap
}

// RateLimit allows burst usage but caps sustained per-user request volume so a
// single lab batch cannot starve the GPU.
func (g *Gateway) RateLimit(ctx context.Context, userID *uuid.UUID, perMinute int) error {
	if userID == nil || perMinute <= 0 {
		return nil
	}
	key := fmt.Sprintf("llm:rate:%s:%d", userID.String(), time.Now().Unix()/60)
	n, err := g.rdb.Incr(ctx, key).Result()
	if err != nil {
		return nil
	}
	g.rdb.Expire(ctx, key, 2*time.Minute)
	if int(n) > perMinute {
		return fmt.Errorf("LLM rate limit reached (%d requests/minute); wait a moment", perMinute)
	}
	return nil
}

// -------------------------------------------------------------------- calls

func (g *Gateway) Chat(ctx context.Context, req Request) (*Result, error) {
	return g.Stream(ctx, req, nil)
}

// Stream performs the call, optionally forwarding tokens as they arrive, and
// always records the full prompt/completion pair for accreditation evidence.
func (g *Gateway) Stream(ctx context.Context, req Request, onChunk func(string) error) (*Result, error) {
	if err := g.CheckBudget(ctx, req.SessionID); err != nil {
		return nil, err
	}
	if err := g.RateLimit(ctx, req.UserID, 30); err != nil {
		return nil, err
	}

	binding, err := g.ModelFor(ctx, req.Module, req.Model)
	if err != nil {
		return nil, err
	}
	version, system := 0, req.System
	if system == "" {
		version, system, err = g.ActivePrompt(ctx, req.Module)
		if err != nil {
			return nil, err
		}
	}

	msgs := make([]Message, 0, len(req.Messages)+1)
	if system != "" {
		msgs = append(msgs, Message{Role: "system", Content: system})
	}
	msgs = append(msgs, req.Messages...)

	temp := req.Temperature
	if temp == 0 {
		temp = 0.2
	}

	start := time.Now()
	var text string
	var usage chatUsage
	opts := chatOptions{
		Endpoint:    binding.Endpoint,
		Runtime:     binding.Runtime,
		Model:       binding.Name,
		Messages:    msgs,
		Temperature: temp,
		MaxTokens:   req.MaxTokens,
		JSONMode:    req.JSONMode,
	}
	if onChunk != nil {
		var sb strings.Builder
		usage, err = g.client.chatStream(ctx, opts, func(chunk string) error {
			sb.WriteString(chunk)
			return onChunk(chunk)
		})
		text = sb.String()
	} else {
		text, usage, err = g.client.chat(ctx, opts)
	}
	latency := int(time.Since(start).Milliseconds())
	tokens := usage.PromptTokens + usage.CompletionTokens

	callID := g.logCall(ctx, req, binding, version, promptTranscript(msgs), text, tokens, latency, err)
	if err != nil {
		return nil, err
	}
	g.addBudget(ctx, req.SessionID, tokens)

	return &Result{
		Text: strings.TrimSpace(text), Model: binding.Name, Endpoint: binding.Endpoint,
		PromptVersion: version, Tokens: tokens, LatencyMS: latency, Module: req.Module,
		CallID: callID, Temperature: temp,
	}, nil
}

func (g *Gateway) Embed(ctx context.Context, text string) ([]float32, error) {
	binding, err := g.ModelFor(ctx, ModuleMITRETagging, "")
	if err != nil {
		return nil, err
	}
	return g.client.embed(ctx, binding.Endpoint, binding.Runtime, g.cfg.LLMEmbedModel, text)
}

func (g *Gateway) ListRemoteModels(ctx context.Context, endpoint string, rt Runtime) ([]string, error) {
	if endpoint == "" {
		endpoint = g.cfg.LLMBaseURL
	}
	if _, err := AssertLocalEndpoint(endpoint, g.cfg.LLMAllowPublicIP); err != nil {
		return nil, err
	}
	if rt == "" {
		rt = RuntimeOllama
	}
	return g.client.Tags(ctx, endpoint, rt)
}

func (g *Gateway) logCall(ctx context.Context, req Request, b ModelBinding, version int, input, output string, tokens, latency int, callErr error) string {
	ok := callErr == nil
	errMsg := ""
	if callErr != nil {
		errMsg = callErr.Error()
	}
	var id uuid.UUID
	// Use a detached context so the record survives a cancelled HTTP request.
	logCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err := g.pool.QueryRow(logCtx, `
		INSERT INTO llm_calls (module, prompt_version, model, endpoint, input, output, tokens, latency_ms, ok, error, user_id, session_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING id`,
		req.Module, version, b.Name, b.Endpoint, input, output, tokens, latency, ok, errMsg, req.UserID, req.SessionID).Scan(&id)
	if err != nil {
		g.log.Error().Err(err).Msg("failed to record llm call")
		return ""
	}
	return id.String()
}

func promptTranscript(msgs []Message) string {
	var sb strings.Builder
	for _, m := range msgs {
		sb.WriteString("[" + m.Role + "]\n")
		sb.WriteString(m.Content)
		sb.WriteString("\n\n")
	}
	return strings.TrimSpace(sb.String())
}

// Health reports whether the local runtime answers, for the admin dashboard.
func (g *Gateway) Health(ctx context.Context) map[string]any {
	binding, err := g.ModelFor(ctx, ModulePentestCopilot, "")
	out := map[string]any{"endpoint": binding.Endpoint, "model": binding.Name, "egress": g.assertion}
	if err != nil {
		out["ok"] = false
		out["error"] = err.Error()
		return out
	}
	models, err := g.client.Tags(ctx, binding.Endpoint, binding.Runtime)
	if err != nil {
		out["ok"] = false
		out["error"] = err.Error()
		return out
	}
	out["ok"] = true
	out["available_models"] = models
	return out
}
