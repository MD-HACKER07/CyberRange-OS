package server

import (
	"context"
	"strings"

	"github.com/cyberrange-os/api/internal/store"
)

func extractJSON(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return s
}

// computeXP rewards difficulty and penalizes heavy copilot reliance, per the
// gamification spec: higher points for less-assisted solves.
func (h *rangeHandler) computeXP(ctx context.Context, sess *store.RangeSession, assistanceRatio float64) int {
	ex, err := h.d.store.GetExercise(ctx, sess.ExerciseID)
	base := 100
	difficulty := 2
	if err == nil {
		difficulty = ex.Difficulty
	}
	base = base * difficulty
	// Assistance multiplier: 1.0 when fully independent, down to 0.5 when
	// every action came from the copilot.
	mult := 1.0 - 0.5*assistanceRatio
	// Bonus for actually demonstrating expected techniques.
	demonstrated, _ := h.d.store.SessionTechniques(ctx, sess.ID)
	techBonus := len(demonstrated) * 15
	return int(float64(base)*mult) + techBonus
}

func (h *rangeHandler) awardXP(ctx context.Context, sess *store.RangeSession, xp int) {
	if xp <= 0 {
		return
	}
	batchID, _ := h.d.store.BatchForExercise(ctx, sess.ExerciseID)
	_ = h.d.store.AddXP(ctx, sess.UserID, batchID, "red", xp, "range session completed", "range_session", &sess.ID)
	_ = h.d.store.RecomputeLeaderboard(ctx, batchID)
}
