// Package store holds the data-access layer for courses, batches, exercises,
// enrollments, and the CO/PO matrix used by the accreditation analytics.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("not found")

type Store struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// ------------------------------------------------------------------ courses

type Course struct {
	ID           uuid.UUID `json:"id"`
	Code         string    `json:"code"`
	Name         string    `json:"name"`
	Semester     int       `json:"semester"`
	AcademicYear string    `json:"academic_year"`
	CreatedAt    time.Time `json:"created_at"`
}

func (s *Store) CreateCourse(ctx context.Context, code, name string, semester int, year string) (*Course, error) {
	var c Course
	err := s.pool.QueryRow(ctx, `
		INSERT INTO courses (code, name, semester, academic_year)
		VALUES ($1,$2,$3,$4) RETURNING id, code, name, semester, academic_year, created_at`,
		code, name, semester, year).Scan(&c.ID, &c.Code, &c.Name, &c.Semester, &c.AcademicYear, &c.CreatedAt)
	return &c, err
}

func (s *Store) ListCourses(ctx context.Context) ([]Course, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, code, name, semester, academic_year, created_at FROM courses ORDER BY code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Course{}
	for rows.Next() {
		var c Course
		if err := rows.Scan(&c.ID, &c.Code, &c.Name, &c.Semester, &c.AcademicYear, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) GetCourse(ctx context.Context, id uuid.UUID) (*Course, error) {
	var c Course
	err := s.pool.QueryRow(ctx, `SELECT id, code, name, semester, academic_year, created_at FROM courses WHERE id=$1`, id).
		Scan(&c.ID, &c.Code, &c.Name, &c.Semester, &c.AcademicYear, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &c, err
}

func (s *Store) UpdateCourse(ctx context.Context, id uuid.UUID, name string, semester int, year string) error {
	_, err := s.pool.Exec(ctx, `UPDATE courses SET name=$2, semester=$3, academic_year=$4 WHERE id=$1`, id, name, semester, year)
	return err
}

func (s *Store) DeleteCourse(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM courses WHERE id=$1`, id)
	return err
}

// --------------------------------------------------------- course outcomes

type CourseOutcome struct {
	ID            uuid.UUID    `json:"id"`
	CourseID      uuid.UUID    `json:"course_id"`
	Code          string       `json:"code"`
	Description   string       `json:"description"`
	TargetPercent float64      `json:"target_percent"`
	POMappings    []POMapping  `json:"po_mappings,omitempty"`
	CreatedAt     time.Time    `json:"created_at"`
}

type POMapping struct {
	POCode string  `json:"po_code"`
	Weight float64 `json:"weight"`
}

func (s *Store) CreateCO(ctx context.Context, courseID uuid.UUID, code, desc string, target float64) (*CourseOutcome, error) {
	var co CourseOutcome
	err := s.pool.QueryRow(ctx, `
		INSERT INTO course_outcomes (course_id, code, description, target_percent)
		VALUES ($1,$2,$3,$4) RETURNING id, course_id, code, description, target_percent, created_at`,
		courseID, code, desc, target).Scan(&co.ID, &co.CourseID, &co.Code, &co.Description, &co.TargetPercent, &co.CreatedAt)
	return &co, err
}

func (s *Store) ListCOs(ctx context.Context, courseID uuid.UUID) ([]CourseOutcome, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, course_id, code, description, target_percent, created_at
		FROM course_outcomes WHERE course_id=$1 ORDER BY code`, courseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CourseOutcome{}
	for rows.Next() {
		var co CourseOutcome
		if err := rows.Scan(&co.ID, &co.CourseID, &co.Code, &co.Description, &co.TargetPercent, &co.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, co)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		m, err := s.POMappingsFor(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].POMappings = m
	}
	return out, nil
}

func (s *Store) POMappingsFor(ctx context.Context, coID uuid.UUID) ([]POMapping, error) {
	rows, err := s.pool.Query(ctx, `SELECT po_code, weight FROM po_mapping WHERE co_id=$1 ORDER BY po_code`, coID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []POMapping{}
	for rows.Next() {
		var m POMapping
		if err := rows.Scan(&m.POCode, &m.Weight); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetPOMatrix replaces all PO mappings for a CO in one transaction.
func (s *Store) SetPOMatrix(ctx context.Context, coID uuid.UUID, mappings []POMapping) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM po_mapping WHERE co_id=$1`, coID); err != nil {
		return err
	}
	for _, m := range mappings {
		if _, err := tx.Exec(ctx, `INSERT INTO po_mapping (co_id, po_code, weight) VALUES ($1,$2,$3)`, coID, m.POCode, m.Weight); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) DeleteCO(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM course_outcomes WHERE id=$1`, id)
	return err
}
