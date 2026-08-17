// Package audit writes the append-only platform audit trail. Every
// privileged action (provisioning, grade override, RBAC change, LLM prompt
// edit) lands here, and high-severity entries are also published to the
// Blue Team alert stream so the SOC module dogfoods the platform itself.
package audit

import (
	"context"
	"encoding/json"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

type Severity string

const (
	SevInfo     Severity = "info"
	SevNotice   Severity = "notice"
	SevWarning  Severity = "warning"
	SevCritical Severity = "critical"
)

type Entry struct {
	ActorID    *uuid.UUID
	ActorRole  string
	Action     string
	TargetType string
	TargetID   string
	Severity   Severity
	IP         string
	Metadata   map[string]any
}

type Logger struct {
	pool *pgxpool.Pool
	log  zerolog.Logger
}

func New(pool *pgxpool.Pool, log zerolog.Logger) *Logger {
	return &Logger{pool: pool, log: log}
}

func (l *Logger) Write(ctx context.Context, e Entry) {
	if e.Severity == "" {
		e.Severity = SevInfo
	}
	meta := e.Metadata
	if meta == nil {
		meta = map[string]any{}
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		raw = []byte(`{}`)
	}
	_, err = l.pool.Exec(ctx, `
		INSERT INTO audit_log (actor_id, actor_role, action, target_type, target_id, severity, ip, metadata_json)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		e.ActorID, e.ActorRole, e.Action, e.TargetType, e.TargetID, string(e.Severity), e.IP, raw)
	if err != nil {
		l.log.Error().Err(err).Str("action", e.Action).Msg("audit write failed")
		return
	}
	l.log.Info().
		Str("action", e.Action).
		Str("target_type", e.TargetType).
		Str("target_id", e.TargetID).
		Str("severity", string(e.Severity)).
		Interface("metadata", meta).
		Msg("audit")
}

// FromCtx is a convenience wrapper that pulls actor identity out of the
// Fiber context (set by the auth middleware).
func (l *Logger) FromCtx(c *fiber.Ctx, action, targetType, targetID string, sev Severity, meta map[string]any) {
	e := Entry{
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		Severity:   sev,
		IP:         c.IP(),
		Metadata:   meta,
	}
	if v := c.Locals("user_id"); v != nil {
		if id, ok := v.(uuid.UUID); ok {
			e.ActorID = &id
		}
	}
	if v := c.Locals("user_role"); v != nil {
		if r, ok := v.(string); ok {
			e.ActorRole = r
		}
	}
	l.Write(c.Context(), e)
}
