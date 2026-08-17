package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ------------------------------------------------------------ cert objectives

type CertObjective struct {
	ID          uuid.UUID `json:"id"`
	Cert        string    `json:"cert"`
	Code        string    `json:"code"`
	Domain      string    `json:"domain"`
	Description string    `json:"description"`
}

func (s *Store) ListCertObjectives(ctx context.Context, cert string) ([]CertObjective, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, cert, code, domain, description FROM cert_objectives
		WHERE ($1='' OR cert=$1) ORDER BY cert, code`, cert)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CertObjective{}
	for rows.Next() {
		var o CertObjective
		if err := rows.Scan(&o.ID, &o.Cert, &o.Code, &o.Domain, &o.Description); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// CertProgress computes, per certification, which objective codes a student
// has covered via graded reports on exercises tagged with those codes.
type CertProgress struct {
	Cert            string   `json:"cert"`
	TotalObjectives int      `json:"total_objectives"`
	Covered         int      `json:"covered"`
	CoveredCodes    []string `json:"covered_codes"`
	GapCodes        []string `json:"gap_codes"`
}

func (s *Store) CertProgressForUser(ctx context.Context, userID uuid.UUID) ([]CertProgress, error) {
	// Covered codes: from exercises with graded reports by this user.
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT unnest(ex.cert_objective_codes) AS code
		FROM reports r JOIN exercises ex ON ex.id=r.exercise_id
		WHERE r.user_id=$1 AND r.status='graded'`, userID)
	if err != nil {
		return nil, err
	}
	covered := map[string]bool{}
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			rows.Close()
			return nil, err
		}
		covered[code] = true
	}
	rows.Close()

	objRows, err := s.pool.Query(ctx, `SELECT cert, code FROM cert_objectives ORDER BY cert, code`)
	if err != nil {
		return nil, err
	}
	defer objRows.Close()
	byCert := map[string]*CertProgress{}
	order := []string{}
	for objRows.Next() {
		var cert, code string
		if err := objRows.Scan(&cert, &code); err != nil {
			return nil, err
		}
		p := byCert[cert]
		if p == nil {
			p = &CertProgress{Cert: cert, CoveredCodes: []string{}, GapCodes: []string{}}
			byCert[cert] = p
			order = append(order, cert)
		}
		p.TotalObjectives++
		if covered[code] {
			p.Covered++
			p.CoveredCodes = append(p.CoveredCodes, code)
		} else {
			p.GapCodes = append(p.GapCodes, code)
		}
	}
	out := make([]CertProgress, 0, len(order))
	for _, c := range order {
		out = append(out, *byCert[c])
	}
	return out, objRows.Err()
}

// ------------------------------------------------------------------ playbooks

type Playbook struct {
	ID         uuid.UUID       `json:"id"`
	ExerciseID *uuid.UUID      `json:"exercise_id"`
	Title      string          `json:"title"`
	ContentMD  string          `json:"content_md"`
	StepsJSON  json.RawMessage `json:"steps_json"`
	CreatedAt  time.Time       `json:"created_at"`
}

func (s *Store) CreatePlaybook(ctx context.Context, exerciseID *uuid.UUID, title, content string, steps json.RawMessage, createdBy *uuid.UUID) (*Playbook, error) {
	if len(steps) == 0 {
		steps = json.RawMessage(`[]`)
	}
	var p Playbook
	err := s.pool.QueryRow(ctx, `
		INSERT INTO playbooks (exercise_id, title, content_md, steps_json, created_by)
		VALUES ($1,$2,$3,$4,$5) RETURNING id, exercise_id, title, content_md, steps_json, created_at`,
		exerciseID, title, content, steps, createdBy).
		Scan(&p.ID, &p.ExerciseID, &p.Title, &p.ContentMD, &p.StepsJSON, &p.CreatedAt)
	return &p, err
}

func (s *Store) PlaybooksForExercise(ctx context.Context, exerciseID uuid.UUID) ([]Playbook, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, exercise_id, title, content_md, steps_json, created_at
		FROM playbooks WHERE exercise_id=$1 ORDER BY created_at`, exerciseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Playbook{}
	for rows.Next() {
		var p Playbook
		if err := rows.Scan(&p.ID, &p.ExerciseID, &p.Title, &p.ContentMD, &p.StepsJSON, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) SetPlaybookProgress(ctx context.Context, playbookID, sessionID uuid.UUID, stepIndex int, done bool, note string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO playbook_progress (playbook_id, session_id, step_index, done, note, updated_at)
		VALUES ($1,$2,$3,$4,$5,now())
		ON CONFLICT (playbook_id, session_id, step_index)
		DO UPDATE SET done=$4, note=$5, updated_at=now()`, playbookID, sessionID, stepIndex, done, note)
	return err
}

// ------------------------------------------------------------------ surveys

type Survey struct {
	ID            uuid.UUID       `json:"id"`
	BatchID       uuid.UUID       `json:"batch_id"`
	Title         string          `json:"title"`
	COIDs         []uuid.UUID     `json:"co_ids"`
	QuestionsJSON json.RawMessage `json:"questions_json"`
	IsOpen        bool            `json:"is_open"`
	CreatedAt     time.Time       `json:"created_at"`
}

func (s *Store) CreateSurvey(ctx context.Context, batchID uuid.UUID, title string, coIDs []uuid.UUID, questions json.RawMessage, createdBy *uuid.UUID) (*Survey, error) {
	if coIDs == nil {
		coIDs = []uuid.UUID{}
	}
	if len(questions) == 0 {
		questions = json.RawMessage(`[]`)
	}
	var sv Survey
	err := s.pool.QueryRow(ctx, `
		INSERT INTO surveys (batch_id, title, co_ids, questions_json, created_by)
		VALUES ($1,$2,$3,$4,$5) RETURNING id, batch_id, title, co_ids, questions_json, is_open, created_at`,
		batchID, title, coIDs, questions, createdBy).
		Scan(&sv.ID, &sv.BatchID, &sv.Title, &sv.COIDs, &sv.QuestionsJSON, &sv.IsOpen, &sv.CreatedAt)
	return &sv, err
}

func (s *Store) ListSurveys(ctx context.Context, batchID uuid.UUID) ([]Survey, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, batch_id, title, co_ids, questions_json, is_open, created_at
		FROM surveys WHERE batch_id=$1 ORDER BY created_at DESC`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Survey{}
	for rows.Next() {
		var sv Survey
		if err := rows.Scan(&sv.ID, &sv.BatchID, &sv.Title, &sv.COIDs, &sv.QuestionsJSON, &sv.IsOpen, &sv.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, sv)
	}
	return out, rows.Err()
}

func (s *Store) SubmitSurveyResponse(ctx context.Context, surveyID, userID uuid.UUID, answers json.RawMessage) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO survey_responses (survey_id, user_id, answers_json)
		VALUES ($1,$2,$3)
		ON CONFLICT (survey_id, user_id) DO UPDATE SET answers_json=$3, submitted_at=now()`,
		surveyID, userID, answers)
	return err
}

// ------------------------------------------------------------ AI redteam scans

type AIScan struct {
	ID            uuid.UUID       `json:"id"`
	Model         string          `json:"model"`
	Endpoint      string          `json:"endpoint"`
	Tool          string          `json:"tool"`
	ProbeCategory string          `json:"probe_category"`
	ProbeName     string          `json:"probe_name"`
	ResultJSON    json.RawMessage `json:"result_json"`
	Passed        bool            `json:"passed"`
	TotalProbes   int             `json:"total_probes"`
	FailedProbes  int             `json:"failed_probes"`
	Status        string          `json:"status"`
	PromptModule  string          `json:"prompt_module"`
	LogOutput     string          `json:"log_output"`
	RunAt         time.Time       `json:"run_at"`
	FinishedAt    *time.Time      `json:"finished_at"`
}

func (s *Store) CreateAIScan(ctx context.Context, model, endpoint, tool, promptModule string, triggeredBy *uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO ai_redteam_scans (model, endpoint, tool, prompt_module, status, triggered_by, result_json)
		VALUES ($1,$2,$3,$4,'running',$5,'{}') RETURNING id`,
		model, endpoint, tool, promptModule, triggeredBy).Scan(&id)
	return id, err
}

func (s *Store) FinishAIScan(ctx context.Context, id uuid.UUID, category string, result json.RawMessage, passed bool, total, failed int, logOut, status string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE ai_redteam_scans SET probe_category=$2, result_json=$3, passed=$4, total_probes=$5, failed_probes=$6, log_output=$7, status=$8, finished_at=now()
		WHERE id=$1`, id, category, result, passed, total, failed, logOut, status)
	return err
}

func (s *Store) ListAIScans(ctx context.Context, limit int) ([]AIScan, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, model, endpoint, tool, probe_category, probe_name, result_json, passed, total_probes, failed_probes, status, prompt_module, log_output, run_at, finished_at
		FROM ai_redteam_scans ORDER BY run_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AIScan{}
	for rows.Next() {
		var a AIScan
		if err := rows.Scan(&a.ID, &a.Model, &a.Endpoint, &a.Tool, &a.ProbeCategory, &a.ProbeName, &a.ResultJSON,
			&a.Passed, &a.TotalProbes, &a.FailedProbes, &a.Status, &a.PromptModule, &a.LogOutput, &a.RunAt, &a.FinishedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
