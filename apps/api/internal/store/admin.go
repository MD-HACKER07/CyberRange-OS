package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------- llm models

type LLMModel struct {
	ID            uuid.UUID  `json:"id"`
	Name          string     `json:"name"`
	Endpoint      string     `json:"endpoint"`
	Runtime       string     `json:"runtime"`
	ContextWindow int        `json:"context_window"`
	Modules       []string   `json:"modules"`
	IsDefault     bool       `json:"is_default"`
	IsActive      bool       `json:"is_active"`
	Notes         string     `json:"notes"`
	CreatedBy     *uuid.UUID `json:"created_by"`
	CreatedAt     time.Time  `json:"created_at"`
}

func (s *Store) ListLLMModels(ctx context.Context) ([]LLMModel, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, endpoint, runtime, context_window, modules, is_default, is_active, notes, created_by, created_at
		FROM llm_models ORDER BY is_default DESC, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LLMModel{}
	for rows.Next() {
		var m LLMModel
		if err := rows.Scan(&m.ID, &m.Name, &m.Endpoint, &m.Runtime, &m.ContextWindow, &m.Modules,
			&m.IsDefault, &m.IsActive, &m.Notes, &m.CreatedBy, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) CreateLLMModel(ctx context.Context, m LLMModel) (*LLMModel, error) {
	if m.Modules == nil {
		m.Modules = []string{}
	}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO llm_models (name, endpoint, runtime, context_window, modules, is_default, is_active, notes, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,true,$7,$8) RETURNING id, created_at`,
		m.Name, m.Endpoint, m.Runtime, m.ContextWindow, m.Modules, m.IsDefault, m.Notes, m.CreatedBy).
		Scan(&m.ID, &m.CreatedAt)
	if err != nil {
		return nil, err
	}
	if m.IsDefault {
		_, _ = s.pool.Exec(ctx, `UPDATE llm_models SET is_default=false WHERE id<>$1`, m.ID)
	}
	m.IsActive = true
	return &m, nil
}

func (s *Store) UpdateLLMModel(ctx context.Context, id uuid.UUID, modules []string, isDefault, isActive bool, notes string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE llm_models SET modules=$2, is_default=$3, is_active=$4, notes=$5 WHERE id=$1`,
		id, modules, isDefault, isActive, notes); err != nil {
		return err
	}
	if isDefault {
		if _, err := tx.Exec(ctx, `UPDATE llm_models SET is_default=false WHERE id<>$1`, id); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) DeleteLLMModel(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM llm_models WHERE id=$1`, id)
	return err
}

// --------------------------------------------------------------- llm prompts

type LLMPrompt struct {
	ID           uuid.UUID  `json:"id"`
	Module       string     `json:"module"`
	Version      int        `json:"version"`
	SystemPrompt string     `json:"system_prompt"`
	Active       bool       `json:"active"`
	Notes        string     `json:"notes"`
	UpdatedBy    *uuid.UUID `json:"updated_by"`
	CreatedAt    time.Time  `json:"created_at"`
}

func (s *Store) ListPrompts(ctx context.Context, module string) ([]LLMPrompt, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, module, version, system_prompt, active, notes, updated_by, created_at
		FROM llm_prompts WHERE ($1='' OR module=$1) ORDER BY module, version DESC`, module)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LLMPrompt{}
	for rows.Next() {
		var p LLMPrompt
		if err := rows.Scan(&p.ID, &p.Module, &p.Version, &p.SystemPrompt, &p.Active, &p.Notes, &p.UpdatedBy, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CreatePromptVersion adds a new version (auto-incremented) and optionally
// activates it, deactivating the previous active version atomically.
func (s *Store) CreatePromptVersion(ctx context.Context, module, systemPrompt, notes string, activate bool, updatedBy *uuid.UUID) (*LLMPrompt, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var version int
	if err := tx.QueryRow(ctx, `SELECT coalesce(max(version),0)+1 FROM llm_prompts WHERE module=$1`, module).Scan(&version); err != nil {
		return nil, err
	}
	if activate {
		if _, err := tx.Exec(ctx, `UPDATE llm_prompts SET active=false WHERE module=$1`, module); err != nil {
			return nil, err
		}
	}
	var p LLMPrompt
	err = tx.QueryRow(ctx, `
		INSERT INTO llm_prompts (module, version, system_prompt, active, notes, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6)
		RETURNING id, module, version, system_prompt, active, notes, updated_by, created_at`,
		module, version, systemPrompt, activate, notes, updatedBy).
		Scan(&p.ID, &p.Module, &p.Version, &p.SystemPrompt, &p.Active, &p.Notes, &p.UpdatedBy, &p.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &p, tx.Commit(ctx)
}

func (s *Store) ActivatePrompt(ctx context.Context, id uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var module string
	if err := tx.QueryRow(ctx, `SELECT module FROM llm_prompts WHERE id=$1`, id).Scan(&module); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE llm_prompts SET active=false WHERE module=$1`, module); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE llm_prompts SET active=true WHERE id=$1`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ------------------------------------------------------------------ audit

type AuditRow struct {
	ID         int64           `json:"id"`
	ActorID    *uuid.UUID      `json:"actor_id"`
	ActorRole  string          `json:"actor_role"`
	ActorName  string          `json:"actor_name"`
	Action     string          `json:"action"`
	TargetType string          `json:"target_type"`
	TargetID   string          `json:"target_id"`
	Severity   string          `json:"severity"`
	IP         string          `json:"ip"`
	Metadata   json.RawMessage `json:"metadata"`
	CreatedAt  time.Time       `json:"created_at"`
}

type AuditFilter struct {
	ActorID  *uuid.UUID
	Action   string
	Severity string
	Since    *time.Time
	Until    *time.Time
	Limit    int
	Offset   int
}

func (s *Store) QueryAudit(ctx context.Context, f AuditFilter) ([]AuditRow, int, error) {
	if f.Limit <= 0 {
		f.Limit = 100
	}
	where := `WHERE ($1::uuid IS NULL OR a.actor_id=$1)
		AND ($2='' OR a.action ILIKE '%'||$2||'%')
		AND ($3='' OR a.severity=$3)
		AND ($4::timestamptz IS NULL OR a.created_at >= $4)
		AND ($5::timestamptz IS NULL OR a.created_at <= $5)`
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log a `+where,
		f.ActorID, f.Action, f.Severity, f.Since, f.Until).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT a.id, a.actor_id, a.actor_role, coalesce(u.name,''), a.action, a.target_type, a.target_id, a.severity, a.ip, a.metadata_json, a.created_at
		FROM audit_log a LEFT JOIN users u ON u.id=a.actor_id `+where+`
		ORDER BY a.id DESC LIMIT $6 OFFSET $7`,
		f.ActorID, f.Action, f.Severity, f.Since, f.Until, f.Limit, f.Offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []AuditRow{}
	for rows.Next() {
		var a AuditRow
		var meta []byte
		if err := rows.Scan(&a.ID, &a.ActorID, &a.ActorRole, &a.ActorName, &a.Action, &a.TargetType, &a.TargetID, &a.Severity, &a.IP, &meta, &a.CreatedAt); err != nil {
			return nil, 0, err
		}
		a.Metadata = meta
		out = append(out, a)
	}
	return out, total, rows.Err()
}

// ------------------------------------------------------------ settings

func (s *Store) GetSetting(ctx context.Context, key string) (json.RawMessage, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT value_json FROM platform_settings WHERE key=$1`, key).Scan(&raw)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func (s *Store) SetSetting(ctx context.Context, key string, value json.RawMessage, updatedBy *uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO platform_settings (key, value_json, updated_by, updated_at)
		VALUES ($1,$2,$3,now())
		ON CONFLICT (key) DO UPDATE SET value_json=$2, updated_by=$3, updated_at=now()`, key, value, updatedBy)
	return err
}
