# CyberSec Model — Dataset Card

**Name:** CyberRange OS CyberSec Instruction Dataset
**Maintainer:** Department of Cyber Security, Sanjivani University (Kopargaon, Maharashtra)
**License:** Released for educational and research use. Attribution appreciated.

## What this is

A supervised instruction-tuning dataset used to specialize a small open-weight
base model into **CyberSec** — the local, department-hosted teaching assistant
that powers CyberRange OS. It teaches the model the *style, format, and domain
voice* of a college SOC / red-team lab, not new secret facts.

## Contents

| File | Records | Purpose |
|------|---------|---------|
| `dataset.jsonl` | ~1,325 | Training split (chat-format instruction/response) |
| `dataset_eval.jsonl` | ~70 | Held-out evaluation split |

Each line is a chat-format JSON object:

```json
{"messages": [
  {"role": "system", "content": "..."},
  {"role": "user", "content": "..."},
  {"role": "assistant", "content": "..."}
]}
```

## Task mix

- **MITRE ATT&CK technique explanation** — concise, citation-style write-ups of
  techniques and sub-techniques (Txxxx / Txxxx.yyy).
- **Activity → technique classification** — map an observed behaviour to a single
  ATT&CK technique, reply as strict JSON.
- **SOC triage voice** — summarise/verdict alerts in the platform's format.
- **Report-grading rubric voice** — score-style feedback for student reports.

Source material for the ATT&CK content derives from the publicly available
[MITRE ATT&CK](https://attack.mitre.org/) knowledge base (STIX).

## Privacy

This dataset contains **no student data and no personal information**. It is
built only from public security knowledge and synthetic teaching examples.
Real institutional data (timetables, rosters, student accounts) is **never**
included here and never leaves the institution network.

## How it was built

1. `build_dataset.py` merges and validates the source `data/*.jsonl` files.
2. QLoRA fine-tune on a free cloud T4 GPU (see `finetune_qlora.ipynb`).
3. Merge adapter + export a quantized **GGUF** (`cyberrange-sec.gguf`).
4. Register the resulting **CyberSec** model in the platform's Admin → LLM
   Registry and assign it to modules.

See `README.md` and `STEPS.md` in this folder for the full recipe.
