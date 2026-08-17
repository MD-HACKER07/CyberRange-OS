package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("not found")

type User struct {
	ID           uuid.UUID  `json:"id"`
	Role         Role       `json:"role"`
	Name         string     `json:"name"`
	RollNo       *string    `json:"roll_no"`
	Email        string     `json:"email"`
	AuthProvider string     `json:"auth_provider"`
	IsActive     bool       `json:"is_active"`
	LastLoginAt  *time.Time `json:"last_login_at"`
	CreatedAt    time.Time  `json:"created_at"`
	Batches      []Batch    `json:"batches,omitempty"`
}

type Batch struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	Term       string    `json:"term"`
	CourseID   uuid.UUID `json:"course_id"`
	CourseCode string    `json:"course_code"`
	CourseName string    `json:"course_name"`
}

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

const userCols = `id, role, name, roll_no, email, auth_provider, is_active, last_login_at, created_at`

func scanUser(row pgx.Row) (*User, error) {
	var u User
	var role string
	err := row.Scan(&u.ID, &role, &u.Name, &u.RollNo, &u.Email, &u.AuthProvider, &u.IsActive, &u.LastLoginAt, &u.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	u.Role = Role(role)
	return &u, nil
}

func (s *Store) ByID(ctx context.Context, id uuid.UUID) (*User, error) {
	return scanUser(s.pool.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE id=$1`, id))
}

// ByLogin accepts either an email address or a roll number.
func (s *Store) ByLogin(ctx context.Context, login string) (*User, string, error) {
	login = strings.ToLower(strings.TrimSpace(login))
	var u User
	var role string
	var hash *string
	err := s.pool.QueryRow(ctx, `
		SELECT id, role, name, roll_no, email, auth_provider, is_active, last_login_at, created_at, password_hash
		FROM users WHERE lower(email)=$1 OR lower(roll_no)=$1`, login).
		Scan(&u.ID, &role, &u.Name, &u.RollNo, &u.Email, &u.AuthProvider, &u.IsActive, &u.LastLoginAt, &u.CreatedAt, &hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", ErrNotFound
		}
		return nil, "", err
	}
	u.Role = Role(role)
	h := ""
	if hash != nil {
		h = *hash
	}
	return &u, h, nil
}

type CreateUserInput struct {
	Role         Role
	Name         string
	Email        string
	RollNo       string
	Password     string
	AuthProvider string
	ExternalID   string
}

func (s *Store) Create(ctx context.Context, in CreateUserInput) (*User, error) {
	var hash *string
	if in.Password != "" {
		h, err := HashPassword(in.Password)
		if err != nil {
			return nil, err
		}
		hash = &h
	}
	var roll *string
	if strings.TrimSpace(in.RollNo) != "" {
		r := strings.TrimSpace(in.RollNo)
		roll = &r
	}
	provider := in.AuthProvider
	if provider == "" {
		provider = "local"
	}
	var extID *string
	if in.ExternalID != "" {
		extID = &in.ExternalID
	}
	return scanUser(s.pool.QueryRow(ctx, `
		INSERT INTO users (role, name, roll_no, email, password_hash, auth_provider, external_id)
		VALUES ($1,$2,$3,lower($4),$5,$6,$7)
		RETURNING `+userCols,
		string(in.Role), strings.TrimSpace(in.Name), roll, in.Email, hash, provider, extID))
}

func (s *Store) UpsertExternal(ctx context.Context, in CreateUserInput) (*User, error) {
	u, _, err := s.ByLogin(ctx, in.Email)
	if err == nil {
		return u, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	return s.Create(ctx, in)
}

func (s *Store) List(ctx context.Context, role string, search string, limit, offset int) ([]User, int, error) {
	where := `WHERE ($1='' OR role=$1) AND ($2='' OR name ILIKE '%'||$2||'%' OR email ILIKE '%'||$2||'%' OR coalesce(roll_no,'') ILIKE '%'||$2||'%')`
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users `+where, role, search).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+userCols+` FROM users `+where+` ORDER BY created_at DESC LIMIT $3 OFFSET $4`,
		role, search, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		var u User
		var r string
		if err := rows.Scan(&u.ID, &r, &u.Name, &u.RollNo, &u.Email, &u.AuthProvider, &u.IsActive, &u.LastLoginAt, &u.CreatedAt); err != nil {
			return nil, 0, err
		}
		u.Role = Role(r)
		out = append(out, u)
	}
	return out, total, rows.Err()
}

func (s *Store) SetRole(ctx context.Context, id uuid.UUID, role Role) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET role=$2, updated_at=now() WHERE id=$1`, id, string(role))
	return err
}

func (s *Store) SetActive(ctx context.Context, id uuid.UUID, active bool) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET is_active=$2, updated_at=now() WHERE id=$1`, id, active)
	return err
}

func (s *Store) SetPassword(ctx context.Context, id uuid.UUID, password string) error {
	h, err := HashPassword(password)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `UPDATE users SET password_hash=$2, updated_at=now() WHERE id=$1`, id, h)
	return err
}

func (s *Store) TouchLogin(ctx context.Context, id uuid.UUID) {
	_, _ = s.pool.Exec(ctx, `UPDATE users SET last_login_at=now() WHERE id=$1`, id)
}

func (s *Store) BatchesFor(ctx context.Context, userID uuid.UUID, role Role) ([]Batch, error) {
	var q string
	switch role {
	case RoleStudent:
		q = `SELECT b.id, b.name, b.term, c.id, c.code, c.name
		     FROM batches b JOIN courses c ON c.id=b.course_id
		     JOIN enrollments e ON e.batch_id=b.id
		     WHERE e.user_id=$1 ORDER BY b.created_at DESC`
	case RoleFaculty:
		q = `SELECT b.id, b.name, b.term, c.id, c.code, c.name
		     FROM batches b JOIN courses c ON c.id=b.course_id
		     WHERE b.faculty_id=$1 ORDER BY b.created_at DESC`
	default:
		// Admin/auditor see every batch. The $1::uuid cast gives Postgres a
		// concrete parameter type so the predicate plans correctly.
		q = `SELECT b.id, b.name, b.term, c.id, c.code, c.name
		     FROM batches b JOIN courses c ON c.id=b.course_id
		     WHERE $1::uuid IS NOT NULL ORDER BY b.created_at DESC`
	}
	rows, err := s.pool.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Batch{}
	for rows.Next() {
		var b Batch
		if err := rows.Scan(&b.ID, &b.Name, &b.Term, &b.CourseID, &b.CourseCode, &b.CourseName); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- refresh

func (s *Store) StoreRefresh(ctx context.Context, userID uuid.UUID, hash string, ttl time.Duration, ua, ip string) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO refresh_tokens (user_id, token_hash, expires_at, user_agent, ip)
		VALUES ($1,$2,now()+$3::interval,$4,$5) RETURNING id`,
		userID, hash, ttl.String(), ua, ip).Scan(&id)
	return id, err
}

type RefreshRecord struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	ExpiresAt time.Time
	RevokedAt *time.Time
}

func (s *Store) FindRefresh(ctx context.Context, hash string) (*RefreshRecord, error) {
	var r RefreshRecord
	err := s.pool.QueryRow(ctx, `
		SELECT id, user_id, expires_at, revoked_at FROM refresh_tokens WHERE token_hash=$1`, hash).
		Scan(&r.ID, &r.UserID, &r.ExpiresAt, &r.RevokedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &r, nil
}

func (s *Store) RotateRefresh(ctx context.Context, oldID uuid.UUID, newID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE refresh_tokens SET revoked_at=now(), rotated_to=$2 WHERE id=$1`, oldID, newID)
	return err
}

func (s *Store) RevokeRefresh(ctx context.Context, hash string) error {
	_, err := s.pool.Exec(ctx, `UPDATE refresh_tokens SET revoked_at=now() WHERE token_hash=$1 AND revoked_at IS NULL`, hash)
	return err
}

func (s *Store) RevokeAllForUser(ctx context.Context, userID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE refresh_tokens SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, userID)
	return err
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n)
	return n, err
}
