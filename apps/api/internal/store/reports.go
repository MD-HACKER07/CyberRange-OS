package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Report struct {
	ID                uuid.UUID       `json:"id"`
	SessionID         *uuid.UUID      `json:"session_id"`
	ExerciseID        *uuid.UUID      `json:"exercise_id"`
	UserID            uuid.UUID       `json:"user_id"`
	Type              string          `json:"type"`
	Title             string          `json:"title"`
	ContentMD         string          `json:"content_md"`
	TechniqueIDs      []string        `json:"technique_ids"`
	AISuggestedScore  *float64        `json:"ai_suggested_score"`
	AIScoreRationale  string          `json:"ai_score_rationale"`
	AIRubricJSON      json.RawMessage `json:"ai_rubric_json"`
	FacultyScore      *float64        `json:"faculty_score"`
	FacultyRubricJSON json.RawMessage `json:"faculty_rubric_json"`
	FacultyFeedback   string          `json:"faculty_feedback"`
	MaxScore          float64         `json:"max_score"`
	Status            string          `json:"status"`
	GradedBy          *uuid.UUID      `json:"graded_by"`
	SubmittedAt       *time.Time      `json:"submitted_at"`
	GradedAt          *time.Time      `json:"graded_at"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

const reportCols = `id, session_id, exercise_id, user_id, type, title, content_md, technique_ids,
	ai_suggested_score, ai_score_rationale, ai_rubric_json, faculty_score, faculty_rubric_json,
	faculty_feedback, max_score, status, graded_by, submitted_at, graded_at, created_at, updated_at`

func scanReport(row pgx.Row) (*Report, error) {
	var r Report
	var aiRubric, facultyRubric []byte
	err := row.Scan(&r.ID, &r.SessionID, &r.ExerciseID, &r.UserID, &r.Type, &r.Title, &r.ContentMD, &r.TechniqueIDs,
		&r.AISuggestedScore, &r.AIScoreRationale, &aiRubric, &r.FacultyScore, &facultyRubric,
		&r.FacultyFeedback, &r.MaxScore, &r.Status, &r.GradedBy, &r.SubmittedAt, &r.GradedAt, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	r.AIRubricJSON = aiRubric
	r.FacultyRubricJSON = facultyRubric
	return &r, nil
}

type ReportInput struct {
	SessionID    *uuid.UUID
	ExerciseID   *uuid.UUID
	UserID       uuid.UUID
	Type         string
	Title        string
	ContentMD    string
	TechniqueIDs []string
	MaxScore     float64
}

func (s *Store) CreateReport(ctx context.Context, in ReportInput) (*Report, error) {
	if in.TechniqueIDs == nil {
		in.TechniqueIDs = []string{}
	}
	if in.MaxScore == 0 {
		in.MaxScore = 100
	}
	return scanReport(s.pool.QueryRow(ctx, `
		INSERT INTO reports (session_id, exercise_id, user_id, type, title, content_md, technique_ids, max_score)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING `+reportCols,
		in.SessionID, in.ExerciseID, in.UserID, in.Type, in.Title, in.ContentMD, in.TechniqueIDs, in.MaxScore))
}

func (s *Store) GetReport(ctx context.Context, id uuid.UUID) (*Report, error) {
	return scanReport(s.pool.QueryRow(ctx, `SELECT `+reportCols+` FROM reports WHERE id=$1`, id))
}

func (s *Store) UpdateReportContent(ctx context.Context, id uuid.UUID, title, content string, techniques []string) (*Report, error) {
	if techniques == nil {
		techniques = []string{}
	}
	return scanReport(s.pool.QueryRow(ctx, `
		UPDATE reports SET title=$2, content_md=$3, technique_ids=$4, updated_at=now()
		WHERE id=$1 AND status IN ('draft','returned') RETURNING `+reportCols,
		id, title, content, techniques))
}

func (s *Store) ListReports(ctx context.Context, userID *uuid.UUID, exerciseID *uuid.UUID, status string, limit, offset int) ([]Report, int, error) {
	if limit <= 0 {
		limit = 50
	}
	where := `WHERE ($1::uuid IS NULL OR user_id=$1) AND ($2::uuid IS NULL OR exercise_id=$2) AND ($3='' OR status=$3)`
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM reports `+where, userID, exerciseID, status).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+reportCols+` FROM reports `+where+` ORDER BY updated_at DESC LIMIT $4 OFFSET $5`,
		userID, exerciseID, status, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Report{}
	for rows.Next() {
		r, err := scanReport(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *r)
	}
	return out, total, rows.Err()
}

// GradingQueue lists submitted reports for batches a faculty member owns.
func (s *Store) GradingQueue(ctx context.Context, facultyID uuid.UUID, isAdmin bool) ([]Report, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+reportCols+` FROM reports r
		WHERE r.status='submitted' AND (
			$2 = true OR EXISTS (
				SELECT 1 FROM exercises ex JOIN batches b ON b.id=ex.batch_id
				WHERE ex.id = r.exercise_id AND b.faculty_id=$1
			))
		ORDER BY r.submitted_at ASC`, facultyID, isAdmin)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Report{}
	for rows.Next() {
		r, err := scanReport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

func (s *Store) SubmitReport(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE reports SET status='submitted', submitted_at=now(), updated_at=now() WHERE id=$1 AND status IN ('draft','returned')`, id)
	return err
}

func (s *Store) SetAIScore(ctx context.Context, id uuid.UUID, score float64, rationale string, rubric json.RawMessage) error {
	_, err := s.pool.Exec(ctx, `UPDATE reports SET ai_suggested_score=$2, ai_score_rationale=$3, ai_rubric_json=$4, updated_at=now() WHERE id=$1`,
		id, score, rationale, rubric)
	return err
}

func (s *Store) GradeReport(ctx context.Context, id, facultyID uuid.UUID, score float64, feedback string, rubric json.RawMessage) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE reports SET faculty_score=$2, faculty_feedback=$3, faculty_rubric_json=$4, graded_by=$5, status='graded', graded_at=now(), updated_at=now()
		WHERE id=$1`, id, score, feedback, rubric, facultyID)
	return err
}

// ---------------------------------------------------------- attachments

func (s *Store) AddAttachment(ctx context.Context, reportID uuid.UUID, filename, path, mime string, size int64) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO report_attachments (report_id, filename, stored_path, mime_type, size_bytes)
		VALUES ($1,$2,$3,$4,$5) RETURNING id`, reportID, filename, path, mime, size).Scan(&id)
	return id, err
}

type Attachment struct {
	ID       uuid.UUID `json:"id"`
	Filename string    `json:"filename"`
	Path     string    `json:"-"`
	MimeType string    `json:"mime_type"`
	Size     int64     `json:"size_bytes"`
}

func (s *Store) Attachments(ctx context.Context, reportID uuid.UUID) ([]Attachment, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, filename, stored_path, mime_type, size_bytes FROM report_attachments WHERE report_id=$1`, reportID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Attachment{}
	for rows.Next() {
		var a Attachment
		if err := rows.Scan(&a.ID, &a.Filename, &a.Path, &a.MimeType, &a.Size); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// FacultyOwnsReport authorizes grading.
func (s *Store) FacultyOwnsReport(ctx context.Context, reportID, facultyID uuid.UUID) (bool, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM reports r JOIN exercises ex ON ex.id=r.exercise_id JOIN batches b ON b.id=ex.batch_id
		WHERE r.id=$1 AND b.faculty_id=$2`, reportID, facultyID).Scan(&n)
	return n > 0, err
}
