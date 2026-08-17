package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Alert struct {
	ID               uuid.UUID       `json:"id"`
	SessionID        *uuid.UUID      `json:"session_id"`
	ExerciseID       *uuid.UUID      `json:"exercise_id"`
	Source           string          `json:"source"`
	ExternalID       *string         `json:"external_id"`
	RuleID           string          `json:"rule_id"`
	RuleDescription  string          `json:"rule_description"`
	Severity         string          `json:"severity"`
	SrcIP            string          `json:"src_ip"`
	DstIP            string          `json:"dst_ip"`
	RawLog           json.RawMessage `json:"raw_log"`
	MitreTechniqueID *string         `json:"mitre_technique_id"`
	EventAt          time.Time       `json:"event_at"`
	DetectedAt       *time.Time      `json:"detected_at"`
	ResolvedAt       *time.Time      `json:"resolved_at"`
	ResolutionNote   string          `json:"resolution_note"`
	GroundTruthLabel *string         `json:"ground_truth_label"`
	AISuggestedLabel *string         `json:"ai_suggested_label"`
	AIConfidence     *float64        `json:"ai_confidence"`
	StudentLabel     *string         `json:"student_label"`
	CreatedAt        time.Time       `json:"created_at"`
}

const alertCols = `id, session_id, exercise_id, source, external_id, rule_id, rule_description, severity,
	src_ip, dst_ip, raw_log, mitre_technique_id, event_at, detected_at, resolved_at, resolution_note,
	ground_truth_label, ai_suggested_label, ai_confidence, student_label, created_at`

func scanAlert(row pgx.Row) (*Alert, error) {
	var a Alert
	var raw []byte
	err := row.Scan(&a.ID, &a.SessionID, &a.ExerciseID, &a.Source, &a.ExternalID, &a.RuleID, &a.RuleDescription,
		&a.Severity, &a.SrcIP, &a.DstIP, &raw, &a.MitreTechniqueID, &a.EventAt, &a.DetectedAt, &a.ResolvedAt,
		&a.ResolutionNote, &a.GroundTruthLabel, &a.AISuggestedLabel, &a.AIConfidence, &a.StudentLabel, &a.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	a.RawLog = raw
	return &a, nil
}

// InsertAlert is idempotent on (source, external_id) so re-polling Wazuh/
// Suricata never duplicates alerts.
func (s *Store) InsertAlert(ctx context.Context, a Alert) (*Alert, bool, error) {
	if len(a.RawLog) == 0 {
		a.RawLog = json.RawMessage(`{}`)
	}
	if a.Severity == "" {
		a.Severity = "medium"
	}
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO siem_alerts (session_id, exercise_id, source, external_id, rule_id, rule_description, severity,
			src_ip, dst_ip, raw_log, mitre_technique_id, event_at, detected_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,now())
		ON CONFLICT (source, external_id) DO NOTHING
		RETURNING id`,
		a.SessionID, a.ExerciseID, a.Source, a.ExternalID, a.RuleID, a.RuleDescription, a.Severity,
		a.SrcIP, a.DstIP, a.RawLog, a.MitreTechniqueID, a.EventAt).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil // duplicate, skipped
	}
	if err != nil {
		return nil, false, err
	}
	a.ID = id
	return &a, true, nil
}

func (s *Store) GetAlert(ctx context.Context, id uuid.UUID) (*Alert, error) {
	return scanAlert(s.pool.QueryRow(ctx, `SELECT `+alertCols+` FROM siem_alerts WHERE id=$1`, id))
}

type AlertFilter struct {
	SessionID  *uuid.UUID
	ExerciseID *uuid.UUID
	Severity   string
	Unresolved bool
	Limit      int
	Offset     int
}

func (s *Store) ListAlerts(ctx context.Context, f AlertFilter) ([]Alert, int, error) {
	if f.Limit <= 0 {
		f.Limit = 50
	}
	where := `WHERE ($1::uuid IS NULL OR session_id=$1)
		AND ($2::uuid IS NULL OR exercise_id=$2)
		AND ($3='' OR severity=$3)
		AND ($4=false OR resolved_at IS NULL)`
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM siem_alerts `+where,
		f.SessionID, f.ExerciseID, f.Severity, f.Unresolved).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+alertCols+` FROM siem_alerts `+where+
		` ORDER BY event_at DESC LIMIT $5 OFFSET $6`,
		f.SessionID, f.ExerciseID, f.Severity, f.Unresolved, f.Limit, f.Offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Alert{}
	for rows.Next() {
		a, err := scanAlert(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *a)
	}
	return out, total, rows.Err()
}

// RecentFromSource returns recent alerts sharing a source IP for copilot context.
func (s *Store) RecentFromSource(ctx context.Context, srcIP string, exclude uuid.UUID, limit int) ([]Alert, error) {
	if srcIP == "" {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT `+alertCols+` FROM siem_alerts
		WHERE src_ip=$1 AND id<>$2 ORDER BY event_at DESC LIMIT $3`, srcIP, exclude, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Alert{}
	for rows.Next() {
		a, err := scanAlert(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (s *Store) SetAISuggestion(ctx context.Context, id uuid.UUID, label string, confidence float64) error {
	_, err := s.pool.Exec(ctx, `UPDATE siem_alerts SET ai_suggested_label=$2, ai_confidence=$3 WHERE id=$1`, id, label, confidence)
	return err
}

func (s *Store) SetGroundTruth(ctx context.Context, id uuid.UUID, label string) error {
	_, err := s.pool.Exec(ctx, `UPDATE siem_alerts SET ground_truth_label=$2 WHERE id=$1`, id, label)
	return err
}

func (s *Store) ResolveAlert(ctx context.Context, id uuid.UUID, userID uuid.UUID, studentLabel, note string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE siem_alerts SET resolved_at=now(), acknowledged_by=$2, student_label=$3, resolution_note=$4 WHERE id=$1`,
		id, userID, studentLabel, note)
	return err
}

// MarkDetected records the first time a student opened/acknowledged an alert
// (used for MTTD). Only sets it once.
func (s *Store) MarkDetected(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE siem_alerts SET detected_at=now() WHERE id=$1 AND detected_at IS NULL`, id)
	return err
}

// ---------------------------------------------------------- SOC accuracy

type SOCAccuracy struct {
	Total           int     `json:"total_labeled"`
	AIAgreeGT        int     `json:"ai_agrees_ground_truth"`
	StudentAgreeGT   int     `json:"student_agrees_ground_truth"`
	StudentAgreeAI   int     `json:"student_agrees_ai"`
	AIAccuracy       float64 `json:"ai_accuracy"`
	StudentAccuracy  float64 `json:"student_accuracy"`
}

func (s *Store) SOCAccuracy(ctx context.Context, exerciseID *uuid.UUID) (*SOCAccuracy, error) {
	var a SOCAccuracy
	err := s.pool.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE ground_truth_label IS NOT NULL),
			count(*) FILTER (WHERE ground_truth_label IS NOT NULL AND ai_suggested_label = ground_truth_label),
			count(*) FILTER (WHERE ground_truth_label IS NOT NULL AND student_label = ground_truth_label),
			count(*) FILTER (WHERE ai_suggested_label IS NOT NULL AND student_label = ai_suggested_label)
		FROM siem_alerts
		WHERE ($1::uuid IS NULL OR exercise_id=$1)`, exerciseID).
		Scan(&a.Total, &a.AIAgreeGT, &a.StudentAgreeGT, &a.StudentAgreeAI)
	if err != nil {
		return nil, err
	}
	if a.Total > 0 {
		a.AIAccuracy = float64(a.AIAgreeGT) / float64(a.Total)
		a.StudentAccuracy = float64(a.StudentAgreeGT) / float64(a.Total)
	}
	return &a, nil
}

// MTTD/MTTR aggregate metrics for the faculty dashboard.
type ResponseMetrics struct {
	MeanTimeToDetectSec  float64 `json:"mttd_seconds"`
	MeanTimeToRespondSec float64 `json:"mttr_seconds"`
	Detected             int     `json:"detected"`
	Resolved             int     `json:"resolved"`
}

func (s *Store) ResponseMetrics(ctx context.Context, exerciseID *uuid.UUID) (*ResponseMetrics, error) {
	var m ResponseMetrics
	err := s.pool.QueryRow(ctx, `
		SELECT
			coalesce(avg(EXTRACT(EPOCH FROM (detected_at - event_at))) FILTER (WHERE detected_at IS NOT NULL),0),
			coalesce(avg(EXTRACT(EPOCH FROM (resolved_at - event_at))) FILTER (WHERE resolved_at IS NOT NULL),0),
			count(*) FILTER (WHERE detected_at IS NOT NULL),
			count(*) FILTER (WHERE resolved_at IS NOT NULL)
		FROM siem_alerts WHERE ($1::uuid IS NULL OR exercise_id=$1)`, exerciseID).
		Scan(&m.MeanTimeToDetectSec, &m.MeanTimeToRespondSec, &m.Detected, &m.Resolved)
	return &m, err
}
