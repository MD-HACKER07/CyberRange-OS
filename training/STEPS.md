# Step-by-Step: Train Your Own Cybersecurity Model (no data of your own needed)

You do **not** need to hand-write a dataset. This guide builds one from public
sources, then fine-tunes a small model on a free cloud GPU.

Total time: ~15 min setup + ~1-3 h unattended training.

---

## Step 0 - What you'll end up with
A small (1.5B-3B) model specialized for: MITRE ATT&CK tagging, security Q&A,
detection guidance, and your platform's JSON output contracts. It runs locally
on the lab box and registers in **Admin -> LLM Registry**.

---

## Step 1 - Install the data tools (on your Windows machine or the server)

```bash
cd training
pip install -r requirements.txt
```

Only `datasets` + `huggingface_hub` are needed here. Training deps install
themselves later in the notebook.

---

## Step 2 - Build the dataset

See what sources are available:
```bash
python prepare_datasets.py --list
```

Build from everything (recommended):
```bash
python prepare_datasets.py
```

Offline-friendly / guaranteed-to-work option (uses only the official MITRE
ATT&CK STIX bundle, which the script downloads directly):
```bash
python prepare_datasets.py --only mitre_stix --max-per-source 100000
```

**Verified result:** the ATT&CK source alone yields **~1,394 examples**
(1,325 train / 69 eval) - already inside the ideal 1k-3k range for a QLoRA run.

Outputs:
- `dataset.jsonl` - training split
- `dataset_eval.jsonl` - held-out eval split (deduplicated, decontaminated)

### Where the data comes from
| Source | What it gives | License note |
|---|---|---|
| Official [MITRE ATT&CK STIX](https://github.com/mitre-attack/attack-stix-data) | technique -> id tagging, explanations, detection guidance | MITRE ATT&CK, CC BY 4.0 - **attribution required** |
| [trendmicro-ailab/Primus-Instruct](https://huggingface.co/datasets/trendmicro-ailab/Primus-Instruct) | ~1k curated cybersecurity QA | check dataset card |
| [dattaraj/security-attacks-MITRE](https://huggingface.co/datasets/dattaraj/security-attacks-MITRE) | scenario -> technique reasoning | check dataset card |
| [CIRCL/vulnerability-attack-techniques](https://huggingface.co/datasets/CIRCL/vulnerability-attack-techniques) | CVE -> expert ATT&CK labels | check dataset card |
| `training/data/*.jsonl` | your own examples (highest value) | yours |

> Verify each dataset's license on its Hugging Face card before institutional
> use. Attribute MITRE ATT&CK in any published report.

---

## Step 3 - Sanity-check the data

```bash
python build_dataset.py --check
head -n 2 dataset.jsonl
```

Spot-read ~10 random lines. Bad labels teach bad behaviour, so it's worth five
minutes. Add any of your own examples to `training/data/*.jsonl` and re-run
Step 2 - your files are merged in automatically and take priority.

---

## Step 4 - Fine-tune on a free GPU

1. Open <https://colab.research.google.com> -> **File -> Upload notebook** ->
   choose `training/finetune_qlora.ipynb`.
2. **Runtime -> Change runtime type -> T4 GPU** (free tier is enough).
3. **Runtime -> Run all**. When the upload cell appears, upload your
   `dataset.jsonl`.
4. Wait ~1-3 hours. The notebook:
   - loads a 1.5B base in 4-bit,
   - attaches LoRA adapters (~1% of params trainable),
   - runs supervised fine-tuning,
   - merges + quantizes,
   - downloads `cyberrange-sec.gguf`.

Base model choice is one line in the notebook:
```python
MODEL = "unsloth/Qwen2.5-1.5B-Instruct-bnb-4bit"   # fastest on CPU inference
# MODEL = "unsloth/Llama-3.2-3B-Instruct-bnb-4bit" # better quality
```

---

## Step 5 - Deploy on the lab box

```bash
scp cyberrange-sec.gguf sanjivani@20.0.3.198:~/CyberRange-OS/training/
```
Then on the server, follow `training/deploy.md`:
```bash
cd ~/CyberRange-OS/training
ollama create cyberrange-sec -f Modelfile
```
Register `cyberrange-sec` in **Admin -> LLM Registry**, assign it to the
modules you trained for, and keep the base model registered as a fallback.

---

## Step 6 - Evaluate before you trust it

Score the fine-tune on `dataset_eval.jsonl` and compare against the base model:
- JSON validity rate (does it always parse?)
- MITRE tagging exact-match accuracy
- Refusal correctness on out-of-scope prompts

Release targets are in `docs/CyberSecLLM.pdf` (Section 9). If the fine-tune
regresses on a task, reassign that module back to the base model in one click.

---

## Troubleshooting

| Symptom | Fix |
|---|---|
| `! skipping <repo>` | That dataset moved/gated. Others still work; or use `--only mitre_stix`. |
| `datasets` not installed | `pip install -r requirements.txt` |
| Colab out-of-memory | Use the 1.5B base, or lower `per_device_train_batch_size` to 1. |
| Colab disconnects | Free tier has limits; re-run, or reduce `num_train_epochs` to 2. |
| Model ignores JSON format | Add more JSON-target examples to `training/data/`, retrain. |

---

## Honest expectations
- This teaches **format, tone, and domain grounding** - the model will reliably
  emit your JSON contracts and cite ATT&CK IDs.
- It does **not** reliably add new facts. Keep using the Assistant knowledge
  base / retrieval for department-specific facts.
- A 1.5B-3B model is not a reasoning powerhouse. Keep the human-in-the-loop
  approval flow exactly as it is.
