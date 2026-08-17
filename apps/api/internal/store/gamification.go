package store

import (
	"context"

	"github.com/google/uuid"
)

func (s *Store) BatchForExercise(ctx context.Context, exerciseID uuid.UUID) (uuid.UUID, error) {
	var bid uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT batch_id FROM exercises WHERE id=$1`, exerciseID).Scan(&bid)
	return bid, err
}

func (s *Store) AddXP(ctx context.Context, userID, batchID uuid.UUID, track string, amount int, reason, refType string, refID *uuid.UUID) error {
	if batchID == uuid.Nil {
		return nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		INSERT INTO xp_events (user_id, batch_id, track, amount, reason, ref_type, ref_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`, userID, batchID, track, amount, reason, refType, refID); err != nil {
		return err
	}
	// Upsert the track-specific total and the combined total.
	for _, t := range []string{track, "combined"} {
		if _, err := tx.Exec(ctx, `
			INSERT INTO leaderboard_entries (user_id, batch_id, track, xp, source)
			VALUES ($1,$2,$3,$4,'platform')
			ON CONFLICT (user_id, batch_id, track, source)
			DO UPDATE SET xp = leaderboard_entries.xp + $4, updated_at=now()`,
			userID, batchID, t, amount); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

type LeaderboardRow struct {
	UserID   uuid.UUID `json:"user_id"`
	Name     string    `json:"name"`
	RollNo   *string   `json:"roll_no"`
	XP       int       `json:"xp"`
	Rank     int       `json:"rank"`
	Track    string    `json:"track"`
	Source   string    `json:"source"`
}

func (s *Store) RecomputeLeaderboard(ctx context.Context, batchID uuid.UUID) error {
	if batchID == uuid.Nil {
		return nil
	}
	// Dense rank within each track by XP.
	_, err := s.pool.Exec(ctx, `
		WITH ranked AS (
			SELECT id, rank() OVER (PARTITION BY track ORDER BY xp DESC) AS r
			FROM leaderboard_entries WHERE batch_id=$1
		)
		UPDATE leaderboard_entries le SET rank = ranked.r
		FROM ranked WHERE le.id = ranked.id`, batchID)
	return err
}

func (s *Store) Leaderboard(ctx context.Context, batchID uuid.UUID, track string) ([]LeaderboardRow, error) {
	if track == "" {
		track = "combined"
	}
	rows, err := s.pool.Query(ctx, `
		SELECT le.user_id, u.name, u.roll_no, le.xp, le.rank, le.track, le.source
		FROM leaderboard_entries le JOIN users u ON u.id=le.user_id
		WHERE le.batch_id=$1 AND le.track=$2
		ORDER BY le.xp DESC LIMIT 100`, batchID, track)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LeaderboardRow{}
	for rows.Next() {
		var r LeaderboardRow
		if err := rows.Scan(&r.UserID, &r.Name, &r.RollNo, &r.XP, &r.Rank, &r.Track, &r.Source); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// UpsertExternalLeaderboard ingests a pluggable CTF-platform feed as an extra
// leaderboard source without becoming a hard dependency.
func (s *Store) UpsertExternalLeaderboard(ctx context.Context, userID, batchID uuid.UUID, source string, xp int) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO leaderboard_entries (user_id, batch_id, track, xp, source)
		VALUES ($1,$2,'combined',$3,$4)
		ON CONFLICT (user_id, batch_id, track, source)
		DO UPDATE SET xp=$3, updated_at=now()`, userID, batchID, xp, source)
	return err
}
