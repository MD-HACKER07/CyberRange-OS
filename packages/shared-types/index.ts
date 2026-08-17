// Shared TypeScript domain types for CyberRange OS. The web app re-exports
// these from src/lib/types.ts; other TS tooling (CLI, integration tests) can
// import them from here. Kept in sync with the Go API response shapes.
export type Role = "student" | "faculty" | "admin" | "auditor";
export type Severity = "critical" | "high" | "medium" | "low" | "info";
export type ExerciseType = "red" | "blue";
export type ReportType = "pentest" | "incident";
export type SessionStatus =
  | "provisioning"
  | "running"
  | "ending"
  | "completed"
  | "failed"
  | "expired";
export type TriageLabel = "true_positive" | "false_positive" | "benign";

export interface ListResponse<T> {
  items: T[];
  total: number;
}

// The full interface definitions live alongside the web app in
// apps/web/src/lib/types.ts to avoid duplication during the build; this
// package pins the shared enums and the generic list envelope that both the
// web app and any auxiliary tooling depend on.
