package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// --------------------------------------------------------------- range targets

type RangeTarget struct {
	ID           uuid.UUID  `json:"id"`
	Slug         string     `json:"slug"`
	Name         string     `json:"name"`
	Description  string     `json:"description"`
	Image        string     `json:"image"`
	ExposedPorts []int32    `json:"exposed_ports"`
	CPUQuota     float64    `json:"cpu_quota"`
	MemoryMB     int        `json:"memory_mb"`
	Privileged   bool       `json:"privileged"`
	IsActive     bool       `json:"is_active"`
	CreatedBy    *uuid.UUID `json:"created_by"`
	CreatedAt    time.Time  `json:"created_at"`
}

func (s *Store) CreateRangeTarget(ctx context.Context, t RangeTarget) (*RangeTarget, error) {
	if t.ExposedPorts == nil {
		t.ExposedPorts = []int32{}
	}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO range_targets (slug, name, description, image, exposed_ports, cpu_quota, memory_mb, privileged, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING id, created_at`,
		t.Slug, t.Name, t.Description, t.Image, t.ExposedPorts, t.CPUQuota, t.MemoryMB, t.Privileged, t.CreatedBy).
		Scan(&t.ID, &t.CreatedAt)
	t.IsActive = true
	return &t, err
}

func (s *Store) ListRangeTargets(ctx context.Context, activeOnly bool) ([]RangeTarget, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, slug, name, description, image, exposed_ports, cpu_quota, memory_mb, privileged, is_active, created_by, created_at
		FROM range_targets WHERE ($1=false OR is_active=true) ORDER BY name`, activeOnly)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RangeTarget{}
	for rows.Next() {
		var t RangeTarget
		if err := rows.Scan(&t.ID, &t.Slug, &t.Name, &t.Description, &t.Image, &t.ExposedPorts,
			&t.CPUQuota, &t.MemoryMB, &t.Privileged, &t.IsActive, &t.CreatedBy, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) GetRangeTargetBySlug(ctx context.Context, slug string) (*RangeTarget, error) {
	var t RangeTarget
	err := s.pool.QueryRow(ctx, `
		SELECT id, slug, name, description, image, exposed_ports, cpu_quota, memory_mb, privileged, is_active, created_by, created_at
		FROM range_targets WHERE slug=$1`, slug).
		Scan(&t.ID, &t.Slug, &t.Name, &t.Description, &t.Image, &t.ExposedPorts,
			&t.CPUQuota, &t.MemoryMB, &t.Privileged, &t.IsActive, &t.CreatedBy, &t.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &t, err
}

func (s *Store) SetRangeTargetActive(ctx context.Context, id uuid.UUID, active bool) error {
	_, err := s.pool.Exec(ctx, `UPDATE range_targets SET is_active=$2 WHERE id=$1`, id, active)
	return err
}

func (s *Store) DeleteRangeTarget(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM range_targets WHERE id=$1`, id)
	return err
}

// ------------------------------------------------------------- range sessions

type RangeSession struct {
	ID              uuid.UUID  `json:"id"`
	ExerciseID      uuid.UUID  `json:"exercise_id"`
	UserID          uuid.UUID  `json:"user_id"`
	Status          string     `json:"status"`
	NetworkID       string     `json:"network_id"`
	NetworkName     string     `json:"network_name"`
	AttackerID      string     `json:"-"`
	AttackerName    string     `json:"attacker_name"`
	TerminalToken   string     `json:"terminal_token,omitempty"`
	Driver          string     `json:"driver"`
	TotalActions    int        `json:"total_actions"`
	AIActions       int        `json:"ai_actions"`
	AssistanceRatio float64    `json:"assistance_ratio"`
	LLMTokensUsed   int        `json:"llm_tokens_used"`
	XPAwarded       int        `json:"xp_awarded"`
	FailureReason   string     `json:"failure_reason,omitempty"`
	ExpiresAt       *time.Time `json:"expires_at"`
	StartedAt       time.Time  `json:"started_at"`
	EndedAt         *time.Time `json:"ended_at"`

	Targets []SessionTarget `json:"targets,omitempty"`
}

type SessionTarget struct {
	ID          uuid.UUID  `json:"id"`
	SessionID   uuid.UUID  `json:"session_id"`
	TargetID    *uuid.UUID `json:"target_id"`
	ContainerID string     `json:"-"`
	Hostname    string     `json:"hostname"`
	IPAddress   string     `json:"ip_address"`
	Image       string     `json:"image"`
	Status      string     `json:"status"`
}

const sessionCols = `id, exercise_id, user_id, status, network_id, network_name, attacker_id, attacker_name,
	terminal_token, driver, total_actions, ai_actions, assistance_ratio, llm_tokens_used, xp_awarded,
	failure_reason, expires_at, started_at, ended_at`

func scanSession(row pgx.Row) (*RangeSession, error) {
	var s RangeSession
	err := row.Scan(&s.ID, &s.ExerciseID, &s.UserID, &s.Status, &s.NetworkID, &s.NetworkName, &s.AttackerID,
		&s.AttackerName, &s.TerminalToken, &s.Driver, &s.TotalActions, &s.AIActions, &s.AssistanceRatio,
		&s.LLMTokensUsed, &s.XPAwarded, &s.FailureReason, &s.ExpiresAt, &s.StartedAt, &s.EndedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &s, nil
}

func (s *Store) CreateSession(ctx context.Context, exerciseID, userID uuid.UUID, driver string, expires time.Time) (*RangeSession, error) {
	return scanSession(s.pool.QueryRow(ctx, `
		INSERT INTO range_sessions (exercise_id, user_id, driver, status, expires_at)
		VALUES ($1,$2,$3,'provisioning',$4)
		RETURNING `+sessionCols, exerciseID, userID, driver, expires))
}

func (s *Store) GetSession(ctx context.Context, id uuid.UUID) (*RangeSession, error) {
	sess, err := scanSession(s.pool.QueryRow(ctx, `SELECT `+sessionCols+` FROM range_sessions WHERE id=$1`, id))
	if err != nil {
		return nil, err
	}
	targets, err := s.SessionTargets(ctx, id)
	if err != nil {
		return nil, err
	}
	sess.Targets = targets
	return sess, nil
}

func (s *Store) SessionTargets(ctx context.Context, sessionID uuid.UUID) ([]SessionTarget, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, session_id, target_id, container_id, hostname, ip_address, image, status
		FROM session_targets WHERE session_id=$1 ORDER BY hostname`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SessionTarget{}
	for rows.Next() {
		var t SessionTarget
		if err := rows.Scan(&t.ID, &t.SessionID, &t.TargetID, &t.ContainerID, &t.Hostname, &t.IPAddress, &t.Image, &t.Status); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) AddSessionTarget(ctx context.Context, t SessionTarget) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO session_targets (session_id, target_id, container_id, hostname, ip_address, image, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		t.SessionID, t.TargetID, t.ContainerID, t.Hostname, t.IPAddress, t.Image, t.Status)
	return err
}

func (s *Store) MarkSessionRunning(ctx context.Context, id uuid.UUID, networkID, networkName, attackerID, attackerName, terminalToken string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE range_sessions SET status='running', network_id=$2, network_name=$3, attacker_id=$4, attacker_name=$5, terminal_token=$6
		WHERE id=$1`, id, networkID, networkName, attackerID, attackerName, terminalToken)
	return err
}

func (s *Store) MarkSessionFailed(ctx context.Context, id uuid.UUID, reason string) error {
	_, err := s.pool.Exec(ctx, `UPDATE range_sessions SET status='failed', failure_reason=$2, ended_at=now() WHERE id=$1`, id, reason)
	return err
}

func (s *Store) MarkSessionEnded(ctx context.Context, id uuid.UUID, status string, ratio float64, xp int) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE range_sessions SET status=$2, assistance_ratio=$3, xp_awarded=$4, ended_at=now() WHERE id=$1`,
		id, status, ratio, xp)
	return err
}

func (s *Store) ActiveSessionForUser(ctx context.Context, userID uuid.UUID) (*RangeSession, error) {
	sess, err := scanSession(s.pool.QueryRow(ctx, `
		SELECT `+sessionCols+` FROM range_sessions
		WHERE user_id=$1 AND status IN ('provisioning','running','ending')
		ORDER BY started_at DESC LIMIT 1`, userID))
	if err != nil {
		return nil, err
	}
	sess.Targets, _ = s.SessionTargets(ctx, sess.ID)
	return sess, nil
}

func (s *Store) ListSessions(ctx context.Context, userID *uuid.UUID, exerciseID *uuid.UUID, limit, offset int) ([]RangeSession, int, error) {
	var total int
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM range_sessions
		WHERE ($1::uuid IS NULL OR user_id=$1) AND ($2::uuid IS NULL OR exercise_id=$2)`, userID, exerciseID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+sessionCols+` FROM range_sessions
		WHERE ($1::uuid IS NULL OR user_id=$1) AND ($2::uuid IS NULL OR exercise_id=$2)
		ORDER BY started_at DESC LIMIT $3 OFFSET $4`, userID, exerciseID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []RangeSession{}
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *sess)
	}
	return out, total, rows.Err()
}

// ExpiredSessions returns sessions past their time limit still marked active.
func (s *Store) ExpiredSessions(ctx context.Context) ([]RangeSession, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+sessionCols+` FROM range_sessions
		WHERE status IN ('provisioning','running') AND expires_at IS NOT NULL AND expires_at < now()`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RangeSession{}
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *sess)
	}
	return out, rows.Err()
}

func (s *Store) CountActiveSessions(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM range_sessions WHERE status IN ('provisioning','running')`).Scan(&n)
	return n, err
}
