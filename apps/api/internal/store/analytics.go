package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// AttainmentInput carries the configurable direct/indirect weights.
type AttainmentInput struct {
	BatchID        uuid.UUID
	DirectWeight   float64
	IndirectWeight float64
}

// COAttainment is a computed attainment record for one course outcome.
type COAttainment struct {
	COID          uuid.UUID `json:"co_id"`
	COCode        string    `json:"co_code"`
	Description   string    `json:"description"`
	TargetPercent float64   `json:"target_percent"`
	DirectScore   float64   `json:"direct_score"`
	IndirectScore float64   `json:"indirect_score"`
	FinalScore    float64   `json:"final_score"`
	AttainmentLvl int       `json:"attainment_level"`
	SampleSize    int       `json:"sample_size"`
}

// ComputeDirectAttainment averages graded report percentages for exercises
// tagged with each CO in the batch.
func (s *Store) ComputeDirectAttainment(ctx context.Context, batchID uuid.UUID) (map[uuid.UUID]struct {
	Score   float64
	Samples int
}, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT co_id, avg(pct) AS score, count(*) AS samples FROM (
			SELECT unnest(ex.co_ids) AS co_id,
			       (coalesce(r.faculty_score, r.ai_suggested_score) / NULLIF(r.max_score,0)) * 100 AS pct
			FROM reports r
			JOIN exercises ex ON ex.id = r.exercise_id
			WHERE ex.batch_id=$1 AND r.status='graded'
			  AND coalesce(r.faculty_score, r.ai_suggested_score) IS NOT NULL
		) t
		GROUP BY co_id`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]struct {
		Score   float64
		Samples int
	}{}
	for rows.Next() {
		var coID uuid.UUID
		var score float64
		var samples int
		if err := rows.Scan(&coID, &score, &samples); err != nil {
			return nil, err
		}
		out[coID] = struct {
			Score   float64
			Samples int
		}{score, samples}
	}
	return out, rows.Err()
}

// ComputeIndirectAttainment averages normalized survey responses mapped to COs.
func (s *Store) ComputeIndirectAttainment(ctx context.Context, batchID uuid.UUID) (map[uuid.UUID]float64, error) {
	// Surveys store answers as {co_id: rating(1-5)}. Convert to percent.
	rows, err := s.pool.Query(ctx, `
		SELECT co_id, avg(rating)*20 AS pct FROM (
			SELECT (kv.key)::uuid AS co_id, (kv.value)::numeric AS rating
			FROM surveys sv
			JOIN survey_responses sr ON sr.survey_id = sv.id
			JOIN LATERAL jsonb_each_text(sr.answers_json) kv ON true
			WHERE sv.batch_id=$1 AND kv.key ~ '^[0-9a-fA-F-]{36}$'
		) t GROUP BY co_id`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]float64{}
	for rows.Next() {
		var coID uuid.UUID
		var pct float64
		if err := rows.Scan(&coID, &pct); err != nil {
			return nil, err
		}
		out[coID] = pct
	}
	return out, rows.Err()
}

func attainmentLevel(score, target float64) int {
	switch {
	case score >= target+10:
		return 3
	case score >= target:
		return 2
	case score >= target-10:
		return 1
	default:
		return 0
	}
}

// ComputeAndStoreAttainment runs the full CO attainment calc and snapshots it.
func (s *Store) ComputeAndStoreAttainment(ctx context.Context, in AttainmentInput) ([]COAttainment, error) {
	batch, err := s.GetBatch(ctx, in.BatchID)
	if err != nil {
		return nil, err
	}
	cos, err := s.ListCOs(ctx, batch.CourseID)
	if err != nil {
		return nil, err
	}
	direct, err := s.ComputeDirectAttainment(ctx, in.BatchID)
	if err != nil {
		return nil, err
	}
	indirect, err := s.ComputeIndirectAttainment(ctx, in.BatchID)
	if err != nil {
		return nil, err
	}

	out := []COAttainment{}
	for _, co := range cos {
		d := direct[co.ID]
		i := indirect[co.ID]
		final := in.DirectWeight*d.Score + in.IndirectWeight*i
		att := COAttainment{
			COID: co.ID, COCode: co.Code, Description: co.Description, TargetPercent: co.TargetPercent,
			DirectScore: round2(d.Score), IndirectScore: round2(i), FinalScore: round2(final),
			AttainmentLvl: attainmentLevel(final, co.TargetPercent), SampleSize: d.Samples,
		}
		out = append(out, att)
		_, _ = s.pool.Exec(ctx, `
			INSERT INTO attainment_snapshots (co_id, batch_id, direct_score, indirect_score, final_score, attainment_level, sample_size)
			VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			co.ID, in.BatchID, att.DirectScore, att.IndirectScore, att.FinalScore, att.AttainmentLvl, att.SampleSize)
	}
	return out, nil
}

// POAttainment rolls CO attainment up to programme outcomes via the weighted
// mapping matrix.
type POAttainment struct {
	POCode     string  `json:"po_code"`
	Score      float64 `json:"score"`
	Weight     float64 `json:"total_weight"`
	Contribs   int     `json:"contributing_cos"`
}

func (s *Store) ComputePOAttainment(ctx context.Context, coAtt []COAttainment) ([]POAttainment, error) {
	type acc struct {
		weighted float64
		weight   float64
		count    int
	}
	agg := map[string]*acc{}
	for _, co := range coAtt {
		mappings, err := s.POMappingsFor(ctx, co.COID)
		if err != nil {
			return nil, err
		}
		for _, m := range mappings {
			a := agg[m.POCode]
			if a == nil {
				a = &acc{}
				agg[m.POCode] = a
			}
			a.weighted += co.FinalScore * m.Weight
			a.weight += m.Weight
			a.count++
		}
	}
	out := []POAttainment{}
	for po, a := range agg {
		score := 0.0
		if a.weight > 0 {
			score = a.weighted / a.weight
		}
		out = append(out, POAttainment{POCode: po, Score: round2(score), Weight: a.weight, Contribs: a.count})
	}
	return out, nil
}

func round2(f float64) float64 {
	return float64(int64(f*100+0.5)) / 100
}

// --------------------------------------------------------------- NAAC stats

type NAACStats struct {
	TotalStudents     int       `json:"total_students"`
	TotalExercises    int       `json:"total_exercises"`
	RangeSessions     int       `json:"range_sessions"`
	ReportsGraded     int       `json:"reports_graded"`
	AISecurityFindings int      `json:"ai_security_findings"`
	AICallsLogged     int       `json:"ai_calls_logged"`
	GeneratedAt       time.Time `json:"generated_at"`
}

func (s *Store) NAACStats(ctx context.Context) (*NAACStats, error) {
	var n NAACStats
	n.GeneratedAt = time.Now().UTC()
	_ = s.pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE role='student'`).Scan(&n.TotalStudents)
	_ = s.pool.QueryRow(ctx, `SELECT count(*) FROM exercises`).Scan(&n.TotalExercises)
	_ = s.pool.QueryRow(ctx, `SELECT count(*) FROM range_sessions`).Scan(&n.RangeSessions)
	_ = s.pool.QueryRow(ctx, `SELECT count(*) FROM reports WHERE status='graded'`).Scan(&n.ReportsGraded)
	_ = s.pool.QueryRow(ctx, `SELECT count(*) FROM ai_redteam_scans WHERE passed=false`).Scan(&n.AISecurityFindings)
	_ = s.pool.QueryRow(ctx, `SELECT count(*) FROM llm_calls`).Scan(&n.AICallsLogged)
	return &n, nil
}
