package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Batch struct {
	ID         uuid.UUID  `json:"id"`
	CourseID   uuid.UUID  `json:"course_id"`
	FacultyID  *uuid.UUID `json:"faculty_id"`
	Name       string     `json:"name"`
	Term       string     `json:"term"`
	CourseCode string     `json:"course_code,omitempty"`
	CourseName string     `json:"course_name,omitempty"`
	FacultyName string    `json:"faculty_name,omitempty"`
	StudentCount int      `json:"student_count,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

func (s *Store) CreateBatch(ctx context.Context, courseID uuid.UUID, faculty *uuid.UUID, name, term string) (*Batch, error) {
	var b Batch
	err := s.pool.QueryRow(ctx, `
		INSERT INTO batches (course_id, faculty_id, name, term)
		VALUES ($1,$2,$3,$4) RETURNING id, course_id, faculty_id, name, term, created_at`,
		courseID, faculty, name, term).Scan(&b.ID, &b.CourseID, &b.FacultyID, &b.Name, &b.Term, &b.CreatedAt)
	return &b, err
}

func (s *Store) ListBatches(ctx context.Context, courseID *uuid.UUID, facultyID *uuid.UUID) ([]Batch, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT b.id, b.course_id, b.faculty_id, b.name, b.term, c.code, c.name,
		       coalesce(u.name,''),
		       (SELECT count(*) FROM enrollments e WHERE e.batch_id=b.id),
		       b.created_at
		FROM batches b
		JOIN courses c ON c.id=b.course_id
		LEFT JOIN users u ON u.id=b.faculty_id
		WHERE ($1::uuid IS NULL OR b.course_id=$1)
		  AND ($2::uuid IS NULL OR b.faculty_id=$2)
		ORDER BY b.created_at DESC`, courseID, facultyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Batch{}
	for rows.Next() {
		var b Batch
		if err := rows.Scan(&b.ID, &b.CourseID, &b.FacultyID, &b.Name, &b.Term, &b.CourseCode, &b.CourseName,
			&b.FacultyName, &b.StudentCount, &b.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) GetBatch(ctx context.Context, id uuid.UUID) (*Batch, error) {
	var b Batch
	err := s.pool.QueryRow(ctx, `
		SELECT b.id, b.course_id, b.faculty_id, b.name, b.term, c.code, c.name, coalesce(u.name,''), b.created_at
		FROM batches b JOIN courses c ON c.id=b.course_id LEFT JOIN users u ON u.id=b.faculty_id
		WHERE b.id=$1`, id).
		Scan(&b.ID, &b.CourseID, &b.FacultyID, &b.Name, &b.Term, &b.CourseCode, &b.CourseName, &b.FacultyName, &b.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &b, err
}

func (s *Store) AssignFaculty(ctx context.Context, batchID, facultyID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE batches SET faculty_id=$2 WHERE id=$1`, batchID, facultyID)
	return err
}

func (s *Store) Enroll(ctx context.Context, batchID, userID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO enrollments (user_id, batch_id) VALUES ($1,$2)
		ON CONFLICT DO NOTHING`, userID, batchID)
	return err
}

func (s *Store) Unenroll(ctx context.Context, batchID, userID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM enrollments WHERE user_id=$1 AND batch_id=$2`, userID, batchID)
	return err
}

type BatchMember struct {
	ID     uuid.UUID `json:"id"`
	Name   string    `json:"name"`
	Email  string    `json:"email"`
	RollNo *string   `json:"roll_no"`
}

func (s *Store) BatchStudents(ctx context.Context, batchID uuid.UUID) ([]BatchMember, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT u.id, u.name, u.email, u.roll_no
		FROM enrollments e JOIN users u ON u.id=e.user_id
		WHERE e.batch_id=$1 AND u.role='student' ORDER BY u.roll_no NULLS LAST, u.name`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BatchMember{}
	for rows.Next() {
		var m BatchMember
		if err := rows.Scan(&m.ID, &m.Name, &m.Email, &m.RollNo); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// IsEnrolled checks a student's membership; faculty ownership is checked
// separately in the handler layer for authorization.
func (s *Store) IsEnrolled(ctx context.Context, batchID, userID uuid.UUID) (bool, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM enrollments WHERE batch_id=$1 AND user_id=$2`, batchID, userID).Scan(&n)
	return n > 0, err
}

func (s *Store) FacultyOwnsBatch(ctx context.Context, batchID, facultyID uuid.UUID) (bool, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM batches WHERE id=$1 AND faculty_id=$2`, batchID, facultyID).Scan(&n)
	return n > 0, err
}

func (s *Store) DeleteBatch(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM batches WHERE id=$1`, id)
	return err
}
