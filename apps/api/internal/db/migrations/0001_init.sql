-- CyberRange OS — core schema (PostgreSQL 16+; pgvector optional)
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- pgvector powers semantic MITRE auto-tagging. It is optional: if the
-- extension is not installed the platform falls back to lexical search, so we
-- attempt creation but never abort the migration when it is unavailable.
DO $$
BEGIN
    CREATE EXTENSION IF NOT EXISTS "vector";
EXCEPTION WHEN OTHERS THEN
    RAISE NOTICE 'pgvector not available; semantic search will use lexical fallback';
END;
$$;

-- ---------------------------------------------------------------- identity
CREATE TABLE IF NOT EXISTS courses (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code        TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    semester    INT  NOT NULL DEFAULT 1,
    academic_year TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    role          TEXT NOT NULL CHECK (role IN ('student','faculty','admin','auditor')),
    name          TEXT NOT NULL,
    roll_no       TEXT UNIQUE,
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT,
    auth_provider TEXT NOT NULL DEFAULT 'local' CHECK (auth_provider IN ('local','oidc','saml')),
    external_id   TEXT,
    is_active     BOOLEAN NOT NULL DEFAULT TRUE,
    last_login_at TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_users_role ON users(role);

CREATE TABLE IF NOT EXISTS batches (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    course_id  UUID NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    faculty_id UUID REFERENCES users(id) ON DELETE SET NULL,
    name       TEXT NOT NULL,
    term       TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (course_id, name)
);

CREATE TABLE IF NOT EXISTS enrollments (
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    batch_id    UUID NOT NULL REFERENCES batches(id) ON DELETE CASCADE,
    enrolled_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, batch_id)
);

CREATE TABLE IF NOT EXISTS refresh_tokens (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    rotated_to UUID REFERENCES refresh_tokens(id),
    user_agent TEXT NOT NULL DEFAULT '',
    ip         TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_refresh_user ON refresh_tokens(user_id);

-- ------------------------------------------------------------ accreditation
CREATE TABLE IF NOT EXISTS course_outcomes (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    course_id   UUID NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    code        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    target_percent NUMERIC(5,2) NOT NULL DEFAULT 60.00,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (course_id, code)
);

CREATE TABLE IF NOT EXISTS po_mapping (
    co_id   UUID NOT NULL REFERENCES course_outcomes(id) ON DELETE CASCADE,
    po_code TEXT NOT NULL,
    weight  NUMERIC(4,2) NOT NULL DEFAULT 1.00 CHECK (weight >= 0 AND weight <= 3),
    PRIMARY KEY (co_id, po_code)
);

-- ------------------------------------------------------------------- range
CREATE TABLE IF NOT EXISTS range_targets (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug          TEXT NOT NULL UNIQUE,
    name          TEXT NOT NULL,
    description   TEXT NOT NULL DEFAULT '',
    image         TEXT NOT NULL,
    exposed_ports INT[] NOT NULL DEFAULT '{}',
    cpu_quota     NUMERIC(4,2) NOT NULL DEFAULT 1.00,
    memory_mb     INT NOT NULL DEFAULT 1024,
    privileged    BOOLEAN NOT NULL DEFAULT FALSE,
    golden_digest TEXT NOT NULL DEFAULT '',
    is_active     BOOLEAN NOT NULL DEFAULT TRUE,
    created_by    UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS exercises (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    batch_id             UUID NOT NULL REFERENCES batches(id) ON DELETE CASCADE,
    type                 TEXT NOT NULL CHECK (type IN ('red','blue')),
    title                TEXT NOT NULL,
    brief_md             TEXT NOT NULL DEFAULT '',
    rubric_json          JSONB NOT NULL DEFAULT '{"criteria":[]}',
    difficulty           INT NOT NULL DEFAULT 2 CHECK (difficulty BETWEEN 1 AND 5),
    co_ids               UUID[] NOT NULL DEFAULT '{}',
    cert_objective_codes TEXT[] NOT NULL DEFAULT '{}',
    target_image_refs    TEXT[] NOT NULL DEFAULT '{}',
    expected_techniques  TEXT[] NOT NULL DEFAULT '{}',
    paired_exercise_id   UUID REFERENCES exercises(id) ON DELETE SET NULL,
    ai_redteam_enabled   BOOLEAN NOT NULL DEFAULT FALSE,
    time_limit_minutes   INT NOT NULL DEFAULT 90,
    is_published         BOOLEAN NOT NULL DEFAULT FALSE,
    created_by           UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_exercises_batch ON exercises(batch_id);

CREATE TABLE IF NOT EXISTS session_requests (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    exercise_id UUID NOT NULL REFERENCES exercises(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status      TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','approved','rejected')),
    decided_by  UUID REFERENCES users(id) ON DELETE SET NULL,
    decided_at  TIMESTAMPTZ,
    note        TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS range_sessions (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    exercise_id      UUID NOT NULL REFERENCES exercises(id) ON DELETE CASCADE,
    user_id          UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status           TEXT NOT NULL DEFAULT 'provisioning'
                     CHECK (status IN ('provisioning','running','ending','completed','failed','expired')),
    network_id       TEXT NOT NULL DEFAULT '',
    network_name     TEXT NOT NULL DEFAULT '',
    attacker_id      TEXT NOT NULL DEFAULT '',
    attacker_name    TEXT NOT NULL DEFAULT '',
    terminal_token   TEXT NOT NULL DEFAULT '',
    driver           TEXT NOT NULL DEFAULT 'docker',
    total_actions    INT NOT NULL DEFAULT 0,
    ai_actions       INT NOT NULL DEFAULT 0,
    assistance_ratio NUMERIC(5,4) NOT NULL DEFAULT 0,
    llm_tokens_used  INT NOT NULL DEFAULT 0,
    xp_awarded       INT NOT NULL DEFAULT 0,
    failure_reason   TEXT NOT NULL DEFAULT '',
    expires_at       TIMESTAMPTZ,
    started_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    ended_at         TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON range_sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_status ON range_sessions(status);

-- Live target instances belonging to a session (never free-text hosts).
CREATE TABLE IF NOT EXISTS session_targets (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id    UUID NOT NULL REFERENCES range_sessions(id) ON DELETE CASCADE,
    target_id     UUID REFERENCES range_targets(id) ON DELETE SET NULL,
    container_id  TEXT NOT NULL DEFAULT '',
    hostname      TEXT NOT NULL,
    ip_address    TEXT NOT NULL DEFAULT '',
    image         TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT 'starting',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_session_targets_session ON session_targets(session_id);

CREATE TABLE IF NOT EXISTS session_command_log (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id         UUID NOT NULL REFERENCES range_sessions(id) ON DELETE CASCADE,
    seq                INT NOT NULL,
    command            TEXT NOT NULL,
    output             TEXT NOT NULL DEFAULT '',
    exit_code          INT,
    target_hostname    TEXT NOT NULL DEFAULT '',
    mitre_technique_id TEXT,
    was_ai_suggested   BOOLEAN NOT NULL DEFAULT FALSE,
    ai_rationale       TEXT NOT NULL DEFAULT '',
    was_modified       BOOLEAN NOT NULL DEFAULT FALSE,
    duration_ms        INT NOT NULL DEFAULT 0,
    approved_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at       TIMESTAMPTZ,
    UNIQUE (session_id, seq)
);
CREATE INDEX IF NOT EXISTS idx_cmdlog_session ON session_command_log(session_id, seq);

-- Copilot suggestions are stored even when never approved (evidence of
-- human-in-the-loop control).
CREATE TABLE IF NOT EXISTS copilot_suggestions (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id         UUID NOT NULL REFERENCES range_sessions(id) ON DELETE CASCADE,
    command            TEXT NOT NULL,
    rationale          TEXT NOT NULL DEFAULT '',
    mitre_technique_id TEXT,
    tool               TEXT NOT NULL DEFAULT '',
    status             TEXT NOT NULL DEFAULT 'proposed'
                       CHECK (status IN ('proposed','approved','modified','rejected','expired')),
    prompt_version     INT NOT NULL DEFAULT 1,
    model              TEXT NOT NULL DEFAULT '',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    decided_at         TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_suggestions_session ON copilot_suggestions(session_id);

-- --------------------------------------------------------------- blue team
CREATE TABLE IF NOT EXISTS siem_alerts (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id         UUID REFERENCES range_sessions(id) ON DELETE SET NULL,
    exercise_id        UUID REFERENCES exercises(id) ON DELETE SET NULL,
    source             TEXT NOT NULL CHECK (source IN ('wazuh','suricata','platform-audit')),
    external_id        TEXT,
    rule_id            TEXT NOT NULL DEFAULT '',
    rule_description   TEXT NOT NULL DEFAULT '',
    severity           TEXT NOT NULL DEFAULT 'medium'
                       CHECK (severity IN ('critical','high','medium','low','info')),
    src_ip             TEXT NOT NULL DEFAULT '',
    dst_ip             TEXT NOT NULL DEFAULT '',
    raw_log            JSONB NOT NULL DEFAULT '{}',
    mitre_technique_id TEXT,
    event_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    detected_at        TIMESTAMPTZ,
    acknowledged_by    UUID REFERENCES users(id) ON DELETE SET NULL,
    resolved_at        TIMESTAMPTZ,
    resolution_note    TEXT NOT NULL DEFAULT '',
    ground_truth_label TEXT CHECK (ground_truth_label IN ('true_positive','false_positive','benign',NULL)),
    ai_suggested_label TEXT CHECK (ai_suggested_label IN ('true_positive','false_positive','benign',NULL)),
    ai_confidence      NUMERIC(4,3),
    student_label      TEXT CHECK (student_label IN ('true_positive','false_positive','benign',NULL)),
    linked_command_id  UUID REFERENCES session_command_log(id) ON DELETE SET NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (source, external_id)
);
CREATE INDEX IF NOT EXISTS idx_alerts_session ON siem_alerts(session_id);
CREATE INDEX IF NOT EXISTS idx_alerts_event_at ON siem_alerts(event_at DESC);

CREATE TABLE IF NOT EXISTS playbooks (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    exercise_id UUID REFERENCES exercises(id) ON DELETE CASCADE,
    title       TEXT NOT NULL,
    content_md  TEXT NOT NULL DEFAULT '',
    steps_json  JSONB NOT NULL DEFAULT '[]',
    created_by  UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS playbook_progress (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    playbook_id UUID NOT NULL REFERENCES playbooks(id) ON DELETE CASCADE,
    session_id  UUID NOT NULL REFERENCES range_sessions(id) ON DELETE CASCADE,
    step_index  INT NOT NULL,
    done        BOOLEAN NOT NULL DEFAULT FALSE,
    note        TEXT NOT NULL DEFAULT '',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (playbook_id, session_id, step_index)
);

-- ------------------------------------------------------------------- MITRE
CREATE TABLE IF NOT EXISTS mitre_techniques (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    technique_id  TEXT NOT NULL UNIQUE,
    name          TEXT NOT NULL,
    tactic        TEXT NOT NULL DEFAULT '',
    tactics       TEXT[] NOT NULL DEFAULT '{}',
    description   TEXT NOT NULL DEFAULT '',
    is_subtechnique BOOLEAN NOT NULL DEFAULT FALSE,
    parent_id     TEXT,
    url           TEXT NOT NULL DEFAULT '',
    platforms     TEXT[] NOT NULL DEFAULT '{}',
    detection     TEXT NOT NULL DEFAULT '',
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_mitre_tactic ON mitre_techniques(tactic);

-- Add the embedding column only when pgvector is present.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_type WHERE typname = 'vector') THEN
        EXECUTE 'ALTER TABLE mitre_techniques ADD COLUMN IF NOT EXISTS embedding vector(768)';
    END IF;
END;
$$;

-- --------------------------------------------------------------- reporting
CREATE TABLE IF NOT EXISTS reports (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id         UUID REFERENCES range_sessions(id) ON DELETE SET NULL,
    exercise_id        UUID REFERENCES exercises(id) ON DELETE SET NULL,
    user_id            UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type               TEXT NOT NULL CHECK (type IN ('pentest','incident')),
    title              TEXT NOT NULL DEFAULT '',
    content_md         TEXT NOT NULL DEFAULT '',
    technique_ids      TEXT[] NOT NULL DEFAULT '{}',
    ai_suggested_score NUMERIC(6,2),
    ai_score_rationale TEXT NOT NULL DEFAULT '',
    ai_rubric_json     JSONB,
    faculty_score      NUMERIC(6,2),
    faculty_rubric_json JSONB,
    faculty_feedback   TEXT NOT NULL DEFAULT '',
    max_score          NUMERIC(6,2) NOT NULL DEFAULT 100,
    status             TEXT NOT NULL DEFAULT 'draft'
                       CHECK (status IN ('draft','submitted','graded','returned')),
    graded_by          UUID REFERENCES users(id) ON DELETE SET NULL,
    submitted_at       TIMESTAMPTZ,
    graded_at          TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_reports_user ON reports(user_id);
CREATE INDEX IF NOT EXISTS idx_reports_status ON reports(status);

CREATE TABLE IF NOT EXISTS report_attachments (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    report_id   UUID NOT NULL REFERENCES reports(id) ON DELETE CASCADE,
    filename    TEXT NOT NULL,
    stored_path TEXT NOT NULL,
    mime_type   TEXT NOT NULL DEFAULT '',
    size_bytes  BIGINT NOT NULL DEFAULT 0,
    uploaded_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ------------------------------------------------------------- LLM gateway
CREATE TABLE IF NOT EXISTS llm_models (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name           TEXT NOT NULL,
    endpoint       TEXT NOT NULL,
    runtime        TEXT NOT NULL DEFAULT 'ollama' CHECK (runtime IN ('ollama','vllm','tgi')),
    context_window INT NOT NULL DEFAULT 8192,
    modules        TEXT[] NOT NULL DEFAULT '{}',
    is_default     BOOLEAN NOT NULL DEFAULT FALSE,
    is_active      BOOLEAN NOT NULL DEFAULT TRUE,
    notes          TEXT NOT NULL DEFAULT '',
    created_by     UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (name, endpoint)
);

CREATE TABLE IF NOT EXISTS llm_prompts (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    module        TEXT NOT NULL,
    version       INT NOT NULL,
    system_prompt TEXT NOT NULL,
    active        BOOLEAN NOT NULL DEFAULT FALSE,
    notes         TEXT NOT NULL DEFAULT '',
    updated_by    UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (module, version)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_prompts_active ON llm_prompts(module) WHERE active;

CREATE TABLE IF NOT EXISTS llm_calls (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    module         TEXT NOT NULL,
    prompt_version INT NOT NULL DEFAULT 0,
    model          TEXT NOT NULL,
    endpoint       TEXT NOT NULL DEFAULT '',
    input          TEXT NOT NULL,
    output         TEXT NOT NULL DEFAULT '',
    tokens         INT NOT NULL DEFAULT 0,
    latency_ms     INT NOT NULL DEFAULT 0,
    ok             BOOLEAN NOT NULL DEFAULT TRUE,
    error          TEXT NOT NULL DEFAULT '',
    user_id        UUID REFERENCES users(id) ON DELETE SET NULL,
    session_id     UUID REFERENCES range_sessions(id) ON DELETE SET NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_llmcalls_created ON llm_calls(created_at DESC);

-- ------------------------------------------------------------- AI security
CREATE TABLE IF NOT EXISTS ai_redteam_scans (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    model          TEXT NOT NULL,
    endpoint       TEXT NOT NULL DEFAULT '',
    tool           TEXT NOT NULL CHECK (tool IN ('pyrit','garak')),
    probe_category TEXT NOT NULL DEFAULT '',
    probe_name     TEXT NOT NULL DEFAULT '',
    result_json    JSONB NOT NULL DEFAULT '{}',
    passed         BOOLEAN NOT NULL DEFAULT FALSE,
    total_probes   INT NOT NULL DEFAULT 0,
    failed_probes  INT NOT NULL DEFAULT 0,
    status         TEXT NOT NULL DEFAULT 'completed'
                   CHECK (status IN ('queued','running','completed','failed')),
    triggered_by   UUID REFERENCES users(id) ON DELETE SET NULL,
    prompt_module  TEXT NOT NULL DEFAULT '',
    log_output     TEXT NOT NULL DEFAULT '',
    run_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at    TIMESTAMPTZ
);

-- ------------------------------------------------------------- attainment
CREATE TABLE IF NOT EXISTS attainment_snapshots (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    co_id         UUID NOT NULL REFERENCES course_outcomes(id) ON DELETE CASCADE,
    batch_id      UUID NOT NULL REFERENCES batches(id) ON DELETE CASCADE,
    direct_score  NUMERIC(6,2) NOT NULL DEFAULT 0,
    indirect_score NUMERIC(6,2) NOT NULL DEFAULT 0,
    final_score   NUMERIC(6,2) NOT NULL DEFAULT 0,
    attainment_level INT NOT NULL DEFAULT 0,
    sample_size   INT NOT NULL DEFAULT 0,
    computed_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_attainment_batch ON attainment_snapshots(batch_id, co_id);

CREATE TABLE IF NOT EXISTS surveys (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    batch_id   UUID NOT NULL REFERENCES batches(id) ON DELETE CASCADE,
    title      TEXT NOT NULL,
    co_ids     UUID[] NOT NULL DEFAULT '{}',
    questions_json JSONB NOT NULL DEFAULT '[]',
    is_open    BOOLEAN NOT NULL DEFAULT TRUE,
    created_by  UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS survey_responses (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    survey_id   UUID NOT NULL REFERENCES surveys(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    answers_json JSONB NOT NULL DEFAULT '{}',
    submitted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (survey_id, user_id)
);

-- ---------------------------------------------------- certification paths
CREATE TABLE IF NOT EXISTS cert_objectives (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    cert        TEXT NOT NULL,
    code        TEXT NOT NULL,
    domain      TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    UNIQUE (cert, code)
);

-- ------------------------------------------------------------ gamification
CREATE TABLE IF NOT EXISTS leaderboard_entries (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    batch_id   UUID NOT NULL REFERENCES batches(id) ON DELETE CASCADE,
    track      TEXT NOT NULL DEFAULT 'combined' CHECK (track IN ('red','blue','combined')),
    xp         INT NOT NULL DEFAULT 0,
    rank       INT NOT NULL DEFAULT 0,
    source     TEXT NOT NULL DEFAULT 'platform',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, batch_id, track, source)
);

CREATE TABLE IF NOT EXISTS xp_events (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    batch_id   UUID REFERENCES batches(id) ON DELETE CASCADE,
    track      TEXT NOT NULL DEFAULT 'combined',
    amount     INT NOT NULL,
    reason     TEXT NOT NULL DEFAULT '',
    ref_type   TEXT NOT NULL DEFAULT '',
    ref_id     UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- -------------------------------------------------------------- audit log
CREATE TABLE IF NOT EXISTS audit_log (
    id            BIGSERIAL PRIMARY KEY,
    actor_id      UUID REFERENCES users(id) ON DELETE SET NULL,
    actor_role    TEXT NOT NULL DEFAULT '',
    action        TEXT NOT NULL,
    target_type   TEXT NOT NULL DEFAULT '',
    target_id     TEXT NOT NULL DEFAULT '',
    severity      TEXT NOT NULL DEFAULT 'info',
    ip            TEXT NOT NULL DEFAULT '',
    metadata_json JSONB NOT NULL DEFAULT '{}',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_audit_created ON audit_log(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_actor ON audit_log(actor_id);

-- append-only enforcement: block UPDATE/DELETE on audit_log
CREATE OR REPLACE FUNCTION audit_log_immutable() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'audit_log is append-only';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_audit_log_immutable ON audit_log;
CREATE TRIGGER trg_audit_log_immutable
    BEFORE UPDATE OR DELETE ON audit_log
    FOR EACH ROW EXECUTE FUNCTION audit_log_immutable();

-- ------------------------------------------------------- platform settings
CREATE TABLE IF NOT EXISTS platform_settings (
    key        TEXT PRIMARY KEY,
    value_json JSONB NOT NULL DEFAULT '{}',
    updated_by UUID REFERENCES users(id) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
