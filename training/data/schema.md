# Training Data Format

Each line in a `.jsonl` file is one training example in **chat** format:

```json
{"messages":[{"role":"system","content":"..."},{"role":"user","content":"..."},{"role":"assistant","content":"..."}]}
```

Rules:
- One JSON object per line (JSONL), UTF-8, no trailing commas.
- `system` is optional but recommended; keep it consistent with the platform persona.
- `assistant` is the target the model learns to produce. Make it exactly the
  style/format you want in production (concise, cite MITRE IDs, JSON where the
  module expects JSON).
- Cover the modules you care about: pentest copilot, SOC copilot, report
  grading, MITRE tagging, and department Q&A.
- Do NOT include real secrets, real student PII, or real exploit payloads.
  Use the lab's intentionally-vulnerable images and generic placeholders.

## Suggested mix (aim ~1,000-3,000 total)
| Category | Examples | What it teaches |
|----------|----------|-----------------|
| Pentest next-step (JSON) | 300+ | Propose one safe command + MITRE id, matching the copilot's JSON contract |
| SOC alert triage (JSON) | 300+ | Summary + verdict + next step in the SOC JSON contract |
| Report grading (JSON) | 150+ | Rubric-based scoring voice |
| MITRE tagging (JSON) | 150+ | Map activity -> technique id |
| Department Q&A (text) | 200+ | Routine/timings/how-to tone |
| Refusals / safety | 100+ | Decline out-of-scope or unsafe requests politely |

## Matching the platform's JSON contracts
For the copilots, make the assistant output match what the API parses. Example
(pentest): the assistant content should be a JSON object with
`rationale`, `command`, `tool`, `mitre_technique_id`, `expected_outcome`.
