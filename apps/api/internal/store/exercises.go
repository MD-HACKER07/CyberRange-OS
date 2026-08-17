package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Exercise struct {
	ID                 uuid.UUID       `json:"id"`
	BatchID            uuid.UUID       `json:"batch_id"`
	Type               string          `json:"type"` // red | blue
	Title              string          `json:"title"`
	BriefMD            string          `json:"brief_md"`
	RubricJSON         json.RawMessage `json:"rubric_json"`
	Difficulty         int             `json:"difficulty"`
	COIDs              []uuid.UUID     `json:"co_ids"`
	CertObjectiveCodes []string        `json:"cert_objective_codes"`
	TargetImageRefs    []string        `json:"target_image_refs"`
	ExpectedTechniques []string        `json:"expected_techniques"`
	PairedExerciseID   *uuid.UUID      `json:"paired_exercise_id"`
	AIRedTeamEnabled   bool            `json:"ai_redteam_enabled"`
	TimeLimitMinutes   int             `json:"time_limit_minutes"`
	IsPublished        bool            `json:"is_published"`
	CreatedBy          *uuid.UUID      `json:"created_by"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
}

const exerciseCols = `id, batch_id, type, title, brief_md, rubric_json, difficulty, co_ids,
	cert_objective_codes, target_image_refs, expected_techniques, paired_exercise_id,
	ai_redteam_enabled, time_limit_minutes, is_published, created_by, created_at, updated_at`

func scanExercise(row pgx.Row) (*Exercise, error) {
	var e Exercise
	var rubric []byte
	err := row.Scan(&e.ID, &e.BatchID, &e.Type, &e.Title, &e.BriefMD, &rubric, &e.Difficulty,
		&e.COIDs, &e.CertObjectiveCodes, &e.TargetImageRefs, &e.ExpectedTechniques, &e.PairedExerciseID,
		&e.AIRedTeamEnabled, &e.TimeLimitMinutes, &e.IsPublished, &e.CreatedBy, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	e.RubricJSON = rubric
	return &e, nil
}

type ExerciseInput struct {
	BatchID            uuid.UUID
	Type               string
	Title              string
	BriefMD            string
	RubricJSON         json.RawMessage
	Difficulty         int
	COIDs              []uuid.UUID
	CertObjectiveCodes []string
	TargetImageRefs    []string
	ExpectedTechniques []string
	PairedExerciseID   *uuid.UUID
	AIRedTeamEnabled   bool
	TimeLimitMinutes   int
	CreatedBy          *uuid.UUID
}

func (s *Store) CreateExercise(ctx context.Context, in ExerciseInput) (*Exercise, error) {
	if len(in.RubricJSON) == 0 {
		in.RubricJSON = json.RawMessage(`{"criteria":[]}`)
	}
	if in.COIDs == nil {
		in.COIDs = []uuid.UUID{}
	}
	if in.CertObjectiveCodes == nil {
		in.CertObjectiveCodes = []string{}
	}
	if in.TargetImageRefs == nil {
		in.TargetImageRefs = []string{}
	}
	if in.ExpectedTechniques == nil {
		in.ExpectedTechniques = []string{}
	}
	if in.Difficulty == 0 {
		in.Difficulty = 2
	}
	if in.TimeLimitMinutes == 0 {
		in.TimeLimitMinutes = 90
	}
	return scanExercise(s.pool.QueryRow(ctx, `
		INSERT INTO exercises (batch_id, type, title, brief_md, rubric_json, difficulty, co_ids,
			cert_objective_codes, target_image_refs, expected_techniques, paired_exercise_id,
			ai_redteam_enabled, time_limit_minutes, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		RETURNING `+exerciseCols,
		in.BatchID, in.Type, in.Title, in.BriefMD, in.RubricJSON, in.Difficulty, in.COIDs,
		in.CertObjectiveCodes, in.TargetImageRefs, in.ExpectedTechniques, in.PairedExerciseID,
		in.AIRedTeamEnabled, in.TimeLimitMinutes, in.CreatedBy))
}

func (s *Store) GetExercise(ctx context.Context, id uuid.UUID) (*Exercise, error) {
	return scanExercise(s.pool.QueryRow(ctx, `SELECT `+exerciseCols+` FROM exercises WHERE id=$1`, id))
}

func (s *Store) ListExercises(ctx context.Context, batchID uuid.UUID, publishedOnly bool, exType string) ([]Exercise, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+exerciseCols+` FROM exercises
		WHERE batch_id=$1 AND ($2=false OR is_published=true) AND ($3='' OR type=$3)
		ORDER BY created_at DESC`, batchID, publishedOnly, exType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Exercise{}
	for rows.Next() {
		e, err := scanExercise(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

func (s *Store) UpdateExercise(ctx context.Context, id uuid.UUID, in ExerciseInput) (*Exercise, error) {
	return scanExercise(s.pool.QueryRow(ctx, `
		UPDATE exercises SET title=$2, brief_md=$3, rubric_json=$4, difficulty=$5, co_ids=$6,
			cert_objective_codes=$7, target_image_refs=$8, expected_techniques=$9,
			paired_exercise_id=$10, ai_redteam_enabled=$11, time_limit_minutes=$12, updated_at=now()
		WHERE id=$1 RETURNING `+exerciseCols,
		id, in.Title, in.BriefMD, in.RubricJSON, in.Difficulty, in.COIDs, in.CertObjectiveCodes,
		in.TargetImageRefs, in.ExpectedTechniques, in.PairedExerciseID, in.AIRedTeamEnabled, in.TimeLimitMinutes))
}

func (s *Store) SetPublished(ctx context.Context, id uuid.UUID, published bool) error {
	_, err := s.pool.Exec(ctx, `UPDATE exercises SET is_published=$2, updated_at=now() WHERE id=$1`, id, published)
	return err
}

func (s *Store) DeleteExercise(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM exercises WHERE id=$1`, id)
	return err
}

// StudentCanAccessExercise verifies the student is enrolled in the exercise's
// batch and that it is published.
func (s *Store) StudentCanAccessExercise(ctx context.Context, exerciseID, userID uuid.UUID) (bool, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM exercises ex
		JOIN enrollments e ON e.batch_id = ex.batch_id
		WHERE ex.id=$1 AND e.user_id=$2 AND ex.is_published=true`, exerciseID, userID).Scan(&n)
	return n > 0, err
}
