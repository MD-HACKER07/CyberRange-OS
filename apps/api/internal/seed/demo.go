package seed

import (
	"context"
	"os"
	"strings"

	"github.com/google/uuid"

	"github.com/cyberrange-os/api/internal/auth"
)

// SeedDemo creates a ready-to-use scenario: a course with CO/PO, a faculty and
// student account, a batch with the student enrolled, and real published Red
// and Blue exercises (plus a Blue playbook). It is fully idempotent so it can
// run on every boot. Credentials come from env with sensible defaults.
func (s *Seeder) SeedDemo(ctx context.Context) error {
	// ---- course ----
	var courseID uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO courses (code, name, semester, academic_year)
		VALUES ('CY-LAB-101','Ethical Hacking & SOC Lab',6,'2025-2026')
		ON CONFLICT (code) DO UPDATE SET name=EXCLUDED.name
		RETURNING id`).Scan(&courseID)
	if err != nil {
		return err
	}

	// ---- course outcomes + PO mapping ----
	cos := []struct{ code, desc string }{
		{"CO1", "Perform reconnaissance and enumeration against target systems."},
		{"CO2", "Identify and exploit common web and network vulnerabilities."},
		{"CO3", "Detect and triage security incidents using SIEM/IDS telemetry."},
		{"CO4", "Document findings and incidents in professional reports."},
	}
	coIDs := map[string]uuid.UUID{}
	for _, co := range cos {
		var id uuid.UUID
		if err := s.pool.QueryRow(ctx, `
			INSERT INTO course_outcomes (course_id, code, description, target_percent)
			VALUES ($1,$2,$3,60)
			ON CONFLICT (course_id, code) DO UPDATE SET description=EXCLUDED.description
			RETURNING id`, courseID, co.code, co.desc).Scan(&id); err != nil {
			return err
		}
		coIDs[co.code] = id
	}
	// Map COs to POs (NBA-style) with weights.
	poMap := map[string][]struct {
		po string
		w  float64
	}{
		"CO1": {{"PO1", 2}, {"PO2", 3}},
		"CO2": {{"PO2", 3}, {"PO3", 2}},
		"CO3": {{"PO2", 2}, {"PO4", 3}},
		"CO4": {{"PO9", 2}, {"PO10", 3}},
	}
	for co, maps := range poMap {
		for _, m := range maps {
			if _, err := s.pool.Exec(ctx, `
				INSERT INTO po_mapping (co_id, po_code, weight) VALUES ($1,$2,$3)
				ON CONFLICT (co_id, po_code) DO UPDATE SET weight=EXCLUDED.weight`,
				coIDs[co], m.po, m.w); err != nil {
				return err
			}
		}
	}

	// ---- faculty + student accounts ----
	facultyID, err := s.upsertUser(ctx, "faculty",
		envOr("SEED_FACULTY_EMAIL", "faculty@cyberrange.local"),
		envOr("SEED_FACULTY_PASSWORD", "Faculty@12345"), "Demo Faculty", "")
	if err != nil {
		return err
	}
	studentID, err := s.upsertUser(ctx, "student",
		envOr("SEED_STUDENT_EMAIL", "student@cyberrange.local"),
		envOr("SEED_STUDENT_PASSWORD", "Student@12345"), "Demo Student", "CY23-001")
	if err != nil {
		return err
	}

	// ---- batch + enrollment ----
	var batchID uuid.UUID
	if err := s.pool.QueryRow(ctx, `
		INSERT INTO batches (course_id, faculty_id, name, term)
		VALUES ($1,$2,'Demo Batch 2026','Even 2026')
		ON CONFLICT (course_id, name) DO UPDATE SET faculty_id=EXCLUDED.faculty_id
		RETURNING id`, courseID, facultyID).Scan(&batchID); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO enrollments (user_id, batch_id) VALUES ($1,$2)
		ON CONFLICT DO NOTHING`, studentID, batchID); err != nil {
		return err
	}

	// ---- exercises ----
	redBrief := "## Scenario\nA vulnerable web application (DVWA) is exposed on the isolated range. " +
		"Perform reconnaissance, enumerate services, identify vulnerabilities, and demonstrate exploitation. " +
		"Use the copilot for guidance, but you must approve every command.\n\n" +
		"## Objectives\n- Map open services on the target\n- Identify at least one web vulnerability\n" +
		"- Demonstrate exploitation (e.g. SQL injection or command injection)\n- Document your steps for the report"
	redRubric := `{"criteria":[
		{"id":"recon","title":"Reconnaissance","description":"Thorough service/port enumeration","max_points":25},
		{"id":"vuln","title":"Vulnerability Identification","description":"Correctly identifies vulnerabilities","max_points":25},
		{"id":"exploit","title":"Exploitation","description":"Demonstrates working exploitation","max_points":30},
		{"id":"report","title":"Documentation","description":"Clear, evidence-backed writeup","max_points":20}]}`
	redID, err := s.upsertExercise(ctx, batchID, "red", "Recon & Exploitation: DVWA", redBrief, redRubric, 2,
		[]uuid.UUID{coIDs["CO1"], coIDs["CO2"], coIDs["CO4"]},
		[]string{"PenTest+ PT0-002 2.2", "PenTest+ PT0-002 3.3", "CEH-14"},
		[]string{"dvwa"},
		[]string{"T1595", "T1046", "T1190", "T1110"}, nil, 90, facultyID)
	if err != nil {
		return err
	}

	juiceBrief := "## Scenario\nOWASP Juice Shop is running on the range. Explore the application, find and exploit " +
		"web vulnerabilities from the OWASP Top 10, and record your methodology.\n\n## Objectives\n" +
		"- Enumerate the web application surface\n- Find an injection or access-control flaw\n- Capture evidence for your report"
	juiceRubric := `{"criteria":[
		{"id":"recon","title":"Enumeration","description":"Maps the application surface","max_points":30},
		{"id":"exploit","title":"Exploitation","description":"Exploits an OWASP Top 10 flaw","max_points":40},
		{"id":"report","title":"Documentation","description":"Professional writeup","max_points":30}]}`
	if _, err := s.upsertExercise(ctx, batchID, "red", "Web Attacks: OWASP Juice Shop", juiceBrief, juiceRubric, 3,
		[]uuid.UUID{coIDs["CO1"], coIDs["CO2"]},
		[]string{"PenTest+ PT0-002 3.3", "CEH-13"},
		[]string{"juice-shop"},
		[]string{"T1595", "T1592", "T1190", "T1213"}, nil, 120, facultyID); err != nil {
		return err
	}

	// Blue exercise paired with the DVWA red exercise.
	blueBrief := "## Scenario\nWhile the Red Team attacks the range, Suricata and Wazuh generate telemetry. " +
		"Triage the incoming alerts, separate true positives from noise, map them to MITRE ATT&CK, and record " +
		"your response. Track your mean-time-to-detect and mean-time-to-respond.\n\n## Objectives\n" +
		"- Identify alerts tied to the web attack\n- Label each alert (TP/FP/benign) with justification\n" +
		"- Write an incident report with root cause and response"
	blueRubric := `{"criteria":[
		{"id":"detect","title":"Detection","description":"Identifies malicious alerts quickly","max_points":30},
		{"id":"triage","title":"Triage Accuracy","description":"Correct TP/FP/benign labels","max_points":35},
		{"id":"response","title":"Incident Response","description":"Sound response and root cause","max_points":35}]}`
	blueID, err := s.upsertExercise(ctx, batchID, "blue", "SOC Triage: Detecting the Web Attack", blueBrief, blueRubric, 2,
		[]uuid.UUID{coIDs["CO3"], coIDs["CO4"]},
		[]string{"Security+ SY0-701 4.4", "Security+ SY0-701 4.8", "CEH-03"},
		nil,
		[]string{"T1595", "T1190", "T1110"}, &redID, 90, facultyID)
	if err != nil {
		return err
	}

	// ---- playbook for the blue exercise ----
	if err := s.upsertPlaybook(ctx, blueID, "Web Attack Detection Playbook",
		"# Web Attack Detection Playbook\n\n"+
			"1. Confirm the source IP and target of the alert.\n"+
			"2. Check for repeated requests / scanning patterns (recon).\n"+
			"3. Correlate with recent alerts from the same source IP.\n"+
			"4. Determine if the payload matches a known signature (SQLi, XSS, path traversal).\n"+
			"5. Label the alert and, if a true positive, document containment steps.\n"+
			"6. Write the incident summary with MTTD/MTTR and root cause.",
		`[{"text":"Confirm source and target"},{"text":"Check for scanning/recon patterns"},
		  {"text":"Correlate related alerts"},{"text":"Identify payload/signature"},
		  {"text":"Label alert (TP/FP/benign)"},{"text":"Document response"}]`,
		facultyID); err != nil {
		return err
	}

	s.log.Info().Msg("demo content ready: course CY-LAB-101, batch Demo Batch 2026, faculty+student accounts, 3 exercises")
	return nil
}

func (s *Seeder) upsertUser(ctx context.Context, role, email, password, name, roll string) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT id FROM users WHERE lower(email)=lower($1)`, email).Scan(&id)
	if err == nil {
		return id, nil
	}
	hash, herr := auth.HashPassword(password)
	if herr != nil {
		return uuid.Nil, herr
	}
	var rollPtr *string
	if strings.TrimSpace(roll) != "" {
		rollPtr = &roll
	}
	err = s.pool.QueryRow(ctx, `
		INSERT INTO users (role, name, roll_no, email, password_hash, auth_provider)
		VALUES ($1,$2,$3,lower($4),$5,'local') RETURNING id`,
		role, name, rollPtr, email, hash).Scan(&id)
	if err == nil {
		s.log.Info().Str("email", email).Str("role", role).Msg("demo account created")
	}
	return id, err
}

func (s *Seeder) upsertExercise(ctx context.Context, batchID uuid.UUID, exType, title, brief, rubric string,
	difficulty int, coIDs []uuid.UUID, certCodes, targets, techniques []string, paired *uuid.UUID,
	timeLimit int, createdBy uuid.UUID) (uuid.UUID, error) {

	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT id FROM exercises WHERE batch_id=$1 AND title=$2`, batchID, title).Scan(&id)
	if err == nil {
		return id, nil // already seeded
	}
	if coIDs == nil {
		coIDs = []uuid.UUID{}
	}
	if certCodes == nil {
		certCodes = []string{}
	}
	if targets == nil {
		targets = []string{}
	}
	if techniques == nil {
		techniques = []string{}
	}
	err = s.pool.QueryRow(ctx, `
		INSERT INTO exercises (batch_id, type, title, brief_md, rubric_json, difficulty, co_ids,
			cert_objective_codes, target_image_refs, expected_techniques, paired_exercise_id,
			time_limit_minutes, is_published, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,true,$13)
		RETURNING id`,
		batchID, exType, title, brief, rubric, difficulty, coIDs, certCodes, targets, techniques, paired,
		timeLimit, createdBy).Scan(&id)
	return id, err
}

func (s *Seeder) upsertPlaybook(ctx context.Context, exerciseID uuid.UUID, title, content, steps string, createdBy uuid.UUID) error {
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM playbooks WHERE exercise_id=$1 AND title=$2`, exerciseID, title).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO playbooks (exercise_id, title, content_md, steps_json, created_by)
		VALUES ($1,$2,$3,$4,$5)`, exerciseID, title, content, steps, createdBy)
	return err
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}
