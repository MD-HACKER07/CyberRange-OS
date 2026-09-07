# CyberRange OS — Custom Small LLM (QLoRA Fine-Tuning Kit)

This kit produces a small, department-specialized model that runs **fully local**
via Ollama and plugs into the platform's LLM registry. Training uses QLoRA on a
free cloud GPU (Colab/Kaggle T4); inference runs on your CPU-only lab box.

## The 4 stages

```
[1] Build dataset  ->  [2] QLoRA fine-tune (cloud GPU)  ->  [3] Export GGUF  ->  [4] ollama create + register
   data/*.jsonl          Unsloth notebook                    merge + quantize     Modelfile on the lab box
```

## Why QLoRA
QLoRA freezes the base model in 4-bit and trains tiny adapter layers, so a
1.5B--3B model fine-tunes in ~1--3 GPU-hours on a single T4 (free Colab). You
then merge the adapter and export a quantized GGUF that Ollama serves on CPU.

## Recommended base models (small, strong, permissive licenses)
| Base | Params | Notes |
|------|--------|-------|
| `Qwen2.5-1.5B-Instruct` | 1.5B | Best size/quality for CPU inference |
| `Llama-3.2-3B-Instruct` | 3B | Higher quality, still CPU-friendly |
| `Qwen2.5-0.5B-Instruct` | 0.5B | Fastest, for very weak hardware |

## Files in this kit
- `data/schema.md` — the exact JSONL training format + rules.
- `data/sample.jsonl` — worked examples (pentest reasoning, SOC triage, department FAQ, report grading).
- `build_dataset.py` — merges/validates your JSONL files into `dataset.jsonl`.
- `finetune_qlora.ipynb` — the Colab/Kaggle notebook (Unsloth) that trains + exports GGUF.
- `Modelfile` — packages the GGUF into an Ollama model with the CyberRange system prompt.
- `deploy.md` — the exact commands to run on the Ubuntu box.

## Quick path
1. Fill `data/*.jsonl` with your own examples (start from `sample.jsonl`).
2. Run `python build_dataset.py` locally to validate and merge.
3. Open `finetune_qlora.ipynb` in Google Colab (Runtime -> T4 GPU), upload `dataset.jsonl`, run all cells. It outputs `cyberrange-sec.gguf`.
4. Copy the GGUF to the lab box, then `ollama create cyberrange-sec -f Modelfile`.
5. Register `cyberrange-sec` in Admin -> LLM Registry, assign it to modules.

## Honest expectations
- Fine-tuning teaches **style, format, and domain grounding** (concise answers,
  MITRE citations, your report rubric voice). It does **not** add large new
  factual knowledge reliably — keep using the knowledge base / RAG for facts.
- Data quality > quantity. 500--3,000 clean examples beats 50k noisy ones.
- Always keep a base model registered as a fallback in the platform.
