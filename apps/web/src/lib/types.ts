// Shared domain types mirroring the Go API responses.
export type Role = "student" | "faculty" | "admin" | "auditor";

export interface Batch {
  id: string;
  name: string;
  term: string;
  course_id: string;
  course_code: string;
  course_name: string;
  student_count?: number;
}

export interface User {
  id: string;
  role: Role;
  name: string;
  roll_no: string | null;
  email: string;
  auth_provider: string;
  is_active: boolean;
  batches?: Batch[];
}

export interface Course {
  id: string;
  code: string;
  name: string;
  semester: number;
  academic_year: string;
}

export interface CourseOutcome {
  id: string;
  course_id: string;
  code: string;
  description: string;
  target_percent: number;
  po_mappings?: { po_code: string; weight: number }[];
}

export interface Exercise {
  id: string;
  batch_id: string;
  type: "red" | "blue";
  title: string;
  brief_md: string;
  rubric_json: unknown;
  difficulty: number;
  co_ids: string[];
  cert_objective_codes: string[];
  target_image_refs: string[];
  expected_techniques: string[];
  ai_redteam_enabled: boolean;
  time_limit_minutes: number;
  is_published: boolean;
}

export interface SessionTarget {
  id: string;
  hostname: string;
  ip_address: string;
  image: string;
  status: string;
}

export interface RangeSession {
  id: string;
  exercise_id: string;
  user_id: string;
  status: "provisioning" | "running" | "ending" | "completed" | "failed" | "expired";
  attacker_name: string;
  driver: string;
  total_actions: number;
  ai_actions: number;
  assistance_ratio: number;
  llm_tokens_used: number;
  xp_awarded: number;
  failure_reason?: string;
  expires_at: string | null;
  started_at: string;
  ended_at: string | null;
  targets?: SessionTarget[];
}

export interface CommandLogEntry {
  id: string;
  seq: number;
  command: string;
  output: string;
  exit_code: number | null;
  target_hostname: string;
  mitre_technique_id: string | null;
  was_ai_suggested: boolean;
  ai_rationale: string;
  was_modified: boolean;
  duration_ms: number;
  approved_at: string;
}

export interface Suggestion {
  id: string;
  session_id: string;
  command: string;
  rationale: string;
  mitre_technique_id: string;
  tool: string;
  status: string;
}

export type Severity = "critical" | "high" | "medium" | "low" | "info";

export interface Alert {
  id: string;
  session_id: string | null;
  source: string;
  rule_id: string;
  rule_description: string;
  severity: Severity;
  src_ip: string;
  dst_ip: string;
  raw_log: unknown;
  mitre_technique_id: string | null;
  event_at: string;
  detected_at: string | null;
  resolved_at: string | null;
  resolution_note: string;
  ground_truth_label: string | null;
  ai_suggested_label: string | null;
  student_label: string | null;
}

export interface Report {
  id: string;
  session_id: string | null;
  exercise_id: string | null;
  user_id: string;
  type: "pentest" | "incident";
  title: string;
  content_md: string;
  technique_ids: string[];
  ai_suggested_score: number | null;
  ai_score_rationale: string;
  faculty_score: number | null;
  faculty_feedback: string;
  max_score: number;
  status: "draft" | "submitted" | "graded" | "returned";
}

export interface RangeTarget {
  id: string;
  slug: string;
  name: string;
  description: string;
  image: string;
  exposed_ports: number[];
  cpu_quota: number;
  memory_mb: number;
  is_active: boolean;
}

export interface LeaderboardRow {
  user_id: string;
  name: string;
  roll_no: string | null;
  xp: number;
  rank: number;
  track: string;
}

export interface LLMModel {
  id: string;
  name: string;
  endpoint: string;
  runtime: string;
  context_window: number;
  modules: string[];
  is_default: boolean;
  is_active: boolean;
  notes: string;
}

export interface EgressAssertion {
  endpoint: string;
  host: string;
  resolved_ips: string[];
  all_private: boolean;
  public_ips?: string[];
  override_in_use: boolean;
  message: string;
}

export interface AIScan {
  id: string;
  model: string;
  tool: string;
  probe_category: string;
  passed: boolean;
  total_probes: number;
  failed_probes: number;
  status: string;
  run_at: string;
  result_json: unknown;
}

export interface ListResponse<T> {
  items: T[];
  total: number;
}
