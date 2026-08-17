package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type CommandLogEntry struct {
	ID               uuid.UUID  `json:"id"`
	SessionID        uuid.UUID  `json:"session_id"`
	Seq              int        `json:"seq"`
	Command          string     `json:"command"`
	Output           string     `json:"output"`
	ExitCode         *int       `json:"exit_code"`
	TargetHostname   string     `json:"target_hostname"`
	MitreTechniqueID *string    `json:"mitre_technique_id"`
	WasAISuggested   bool       `json:"was_ai_suggested"`
	AIRationale      string     `json:"ai_rationale"`
	WasModified      bool       `json:"was_modified"`
	DurationMS       int        `json:"duration_ms"`
	ApprovedAt       time.Time  `json:"approved_at"`
	CompletedAt      *time.Time `json:"completed_at"`
}

func (s *Store) NextCommandSeq(ctx context.Context, sessionID uuid.UUID) (int, error) {
	var seq int
	err := s.pool.QueryRow(ctx, `SELECT coalesce(max(seq),0)+1 FROM session_command_log WHERE session_id=$1`, sessionID).Scan(&seq)
	return seq, err
}

type CommandLogInput struct {
	SessionID      uuid.UUID
	Seq            int
	Command        string
	Output         string
	ExitCode       *int
	TargetHostname string
	Technique      *string
	WasAISuggested bool
	AIRationale    string
	WasModified    bool
	DurationMS     int
}

func (s *Store) InsertCommandLog(ctx context.Context, in CommandLogInput) (*CommandLogEntry, error) {
	var e CommandLogEntry
	err := s.pool.QueryRow(ctx, `
		INSERT INTO session_command_log
			(session_id, seq, command, output, exit_code, target_hostname, mitre_technique_id, was_ai_suggested, ai_rationale, was_modified, duration_ms, completed_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,now())
		RETURNING id, session_id, seq, command, output, exit_code, target_hostname, mitre_technique_id, was_ai_suggested, ai_rationale, was_modified, duration_ms, approved_at, completed_at`,
		in.SessionID, in.Seq, in.Command, in.Output, in.ExitCode, in.TargetHostname, in.Technique,
		in.WasAISuggested, in.AIRationale, in.WasModified, in.DurationMS).
		Scan(&e.ID, &e.SessionID, &e.Seq, &e.Command, &e.Output, &e.ExitCode, &e.TargetHostname,
			&e.MitreTechniqueID, &e.WasAISuggested, &e.AIRationale, &e.WasModified, &e.DurationMS, &e.ApprovedAt, &e.CompletedAt)
	if err != nil {
		return nil, err
	}
	// Keep session action counters in sync for the assistance ratio.
	ai := 0
	if in.WasAISuggested {
		ai = 1
	}
	_, _ = s.pool.Exec(ctx, `UPDATE range_sessions SET total_actions=total_actions+1, ai_actions=ai_actions+$2 WHERE id=$1`, in.SessionID, ai)
	return &e, nil
}

func (s *Store) CommandLog(ctx context.Context, sessionID uuid.UUID) ([]CommandLogEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, session_id, seq, command, output, exit_code, target_hostname, mitre_technique_id, was_ai_suggested, ai_rationale, was_modified, duration_ms, approved_at, completed_at
		FROM session_command_log WHERE session_id=$1 ORDER BY seq`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CommandLogEntry{}
	for rows.Next() {
		var e CommandLogEntry
		if err := rows.Scan(&e.ID, &e.SessionID, &e.Seq, &e.Command, &e.Output, &e.ExitCode, &e.TargetHostname,
			&e.MitreTechniqueID, &e.WasAISuggested, &e.AIRationale, &e.WasModified, &e.DurationMS, &e.ApprovedAt, &e.CompletedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) SetCommandTechnique(ctx context.Context, id uuid.UUID, technique string) error {
	_, err := s.pool.Exec(ctx, `UPDATE session_command_log SET mitre_technique_id=$2 WHERE id=$1`, id, technique)
	return err
}

// SessionTechniques returns the distinct MITRE techniques demonstrated.
func (s *Store) SessionTechniques(ctx context.Context, sessionID uuid.UUID) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT mitre_technique_id FROM session_command_log
		WHERE session_id=$1 AND mitre_technique_id IS NOT NULL AND mitre_technique_id <> ''
		ORDER BY mitre_technique_id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// -------------------------------------------------------- copilot suggestions

type Suggestion struct {
	ID               uuid.UUID `json:"id"`
	SessionID        uuid.UUID `json:"session_id"`
	Command          string    `json:"command"`
	Rationale        string    `json:"rationale"`
	MitreTechniqueID string    `json:"mitre_technique_id"`
	Tool             string    `json:"tool"`
	Status           string    `json:"status"`
	PromptVersion    int       `json:"prompt_version"`
	Model            string    `json:"model"`
	CreatedAt        time.Time `json:"created_at"`
}

func (s *Store) InsertSuggestion(ctx context.Context, sug Suggestion) (*Suggestion, error) {
	err := s.pool.QueryRow(ctx, `
		INSERT INTO copilot_suggestions (session_id, command, rationale, mitre_technique_id, tool, prompt_version, model)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id, status, created_at`,
		sug.SessionID, sug.Command, sug.Rationale, sug.MitreTechniqueID, sug.Tool, sug.PromptVersion, sug.Model).
		Scan(&sug.ID, &sug.Status, &sug.CreatedAt)
	return &sug, err
}

func (s *Store) GetSuggestion(ctx context.Context, id uuid.UUID) (*Suggestion, error) {
	var sug Suggestion
	err := s.pool.QueryRow(ctx, `
		SELECT id, session_id, command, rationale, mitre_technique_id, tool, status, prompt_version, model, created_at
		FROM copilot_suggestions WHERE id=$1`, id).
		Scan(&sug.ID, &sug.SessionID, &sug.Command, &sug.Rationale, &sug.MitreTechniqueID, &sug.Tool,
			&sug.Status, &sug.PromptVersion, &sug.Model, &sug.CreatedAt)
	if err != nil {
		return nil, ErrNotFound
	}
	return &sug, nil
}

func (s *Store) SetSuggestionStatus(ctx context.Context, id uuid.UUID, status string) error {
	_, err := s.pool.Exec(ctx, `UPDATE copilot_suggestions SET status=$2, decided_at=now() WHERE id=$1`, id, status)
	return err
}
