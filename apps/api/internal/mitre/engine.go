// Package mitre owns the ATT&CK dataset: ingestion of the public STIX/JSON
// bundle, pgvector embeddings of technique descriptions, and semantic
// auto-tagging of command logs and SIEM alerts.
package mitre

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/cyberrange-os/api/internal/llm"
)

type Engine struct {
	pool    *pgxpool.Pool
	gateway *llm.Gateway
	log     zerolog.Logger
}

func NewEngine(pool *pgxpool.Pool, gateway *llm.Gateway, log zerolog.Logger) *Engine {
	return &Engine{pool: pool, gateway: gateway, log: log}
}

type Technique struct {
	TechniqueID    string   `json:"technique_id"`
	Name           string   `json:"name"`
	Tactic         string   `json:"tactic"`
	Tactics        []string `json:"tactics"`
	Description    string   `json:"description"`
	IsSubtechnique bool     `json:"is_subtechnique"`
	URL            string   `json:"url"`
	Platforms      []string `json:"platforms"`
	Detection      string   `json:"detection"`
	Score          float64  `json:"score,omitempty"`
}

func (e *Engine) Count(ctx context.Context) (int, error) {
	var n int
	err := e.pool.QueryRow(ctx, `SELECT count(*) FROM mitre_techniques`).Scan(&n)
	return n, err
}

func (e *Engine) Get(ctx context.Context, techniqueID string) (*Technique, error) {
	var t Technique
	err := e.pool.QueryRow(ctx, `
		SELECT technique_id, name, tactic, tactics, description, is_subtechnique, url, platforms, detection
		FROM mitre_techniques WHERE technique_id=$1`, strings.ToUpper(techniqueID)).
		Scan(&t.TechniqueID, &t.Name, &t.Tactic, &t.Tactics, &t.Description, &t.IsSubtechnique, &t.URL, &t.Platforms, &t.Detection)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &t, nil
}

func (e *Engine) List(ctx context.Context, tactic, search string, limit int) ([]Technique, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := e.pool.Query(ctx, `
		SELECT technique_id, name, tactic, tactics, description, is_subtechnique, url, platforms, detection
		FROM mitre_techniques
		WHERE ($1='' OR tactic=$1 OR $1 = ANY(tactics))
		  AND ($2='' OR name ILIKE '%'||$2||'%' OR technique_id ILIKE '%'||$2||'%')
		ORDER BY technique_id LIMIT $3`, tactic, search, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTechniques(rows)
}

func scanTechniques(rows pgx.Rows) ([]Technique, error) {
	out := []Technique{}
	for rows.Next() {
		var t Technique
		if err := rows.Scan(&t.TechniqueID, &t.Name, &t.Tactic, &t.Tactics, &t.Description,
			&t.IsSubtechnique, &t.URL, &t.Platforms, &t.Detection); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// SemanticSearch embeds the query and finds the nearest technique vectors.
// Falls back to text search when embeddings are unavailable so the platform
// still tags without a running embedding model.
func (e *Engine) SemanticSearch(ctx context.Context, query string, topK int) ([]Technique, error) {
	if topK <= 0 {
		topK = 5
	}
	vec, err := e.gateway.Embed(ctx, query)
	if err != nil || len(vec) == 0 {
		e.log.Debug().Err(err).Msg("embedding unavailable; using lexical technique search")
		return e.lexicalSearch(ctx, query, topK)
	}
	lit := vectorLiteral(vec)
	rows, err := e.pool.Query(ctx, `
		SELECT technique_id, name, tactic, tactics, description, is_subtechnique, url, platforms, detection,
		       1 - (embedding <=> $1::vector) AS score
		FROM mitre_techniques
		WHERE embedding IS NOT NULL
		ORDER BY embedding <=> $1::vector
		LIMIT $2`, lit, topK)
	if err != nil {
		return e.lexicalSearch(ctx, query, topK)
	}
	defer rows.Close()
	out := []Technique{}
	for rows.Next() {
		var t Technique
		if err := rows.Scan(&t.TechniqueID, &t.Name, &t.Tactic, &t.Tactics, &t.Description,
			&t.IsSubtechnique, &t.URL, &t.Platforms, &t.Detection, &t.Score); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (e *Engine) lexicalSearch(ctx context.Context, query string, topK int) ([]Technique, error) {
	terms := strings.Fields(strings.ToLower(query))
	like := "%"
	if len(terms) > 0 {
		like = "%" + terms[0] + "%"
	}
	rows, err := e.pool.Query(ctx, `
		SELECT technique_id, name, tactic, tactics, description, is_subtechnique, url, platforms, detection
		FROM mitre_techniques
		WHERE name ILIKE $1 OR description ILIKE $1
		ORDER BY technique_id LIMIT $2`, like, topK)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTechniques(rows)
}

// Tag classifies a piece of activity to a technique using semantic retrieval
// plus an LLM disambiguation step. Returns technique id and confidence.
func (e *Engine) Tag(ctx context.Context, activity string) (string, float64, error) {
	candidates, err := e.SemanticSearch(ctx, activity, 6)
	if err != nil || len(candidates) == 0 {
		return "", 0, err
	}
	var sb strings.Builder
	for _, c := range candidates {
		desc := c.Description
		if len(desc) > 200 {
			desc = desc[:200]
		}
		sb.WriteString(fmt.Sprintf("- %s (%s): %s\n", c.TechniqueID, c.Name, desc))
	}
	prompt := fmt.Sprintf("Activity to classify:\n%s\n\nCandidate techniques:\n%s", activity, sb.String())
	res, err := e.gateway.Chat(ctx, llm.Request{
		Module:   llm.ModuleMITRETagging,
		Messages: []llm.Message{{Role: "user", Content: prompt}},
		JSONMode: true,
		MaxTokens: 300,
	})
	if err != nil {
		// LLM down: return top semantic hit at reduced confidence.
		return candidates[0].TechniqueID, candidates[0].Score * 0.6, nil
	}
	var parsed struct {
		TechniqueID string  `json:"technique_id"`
		Confidence  float64 `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(extractJSON(res.Text)), &parsed); err != nil {
		return candidates[0].TechniqueID, candidates[0].Score * 0.6, nil
	}
	return strings.ToUpper(strings.TrimSpace(parsed.TechniqueID)), parsed.Confidence, nil
}

func vectorLiteral(vec []float32) string {
	parts := make([]string, len(vec))
	for i, v := range vec {
		parts[i] = strconv.FormatFloat(float64(v), 'f', -1, 32)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func extractJSON(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return s
}
