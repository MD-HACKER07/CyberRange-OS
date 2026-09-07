# Deploy the fine-tuned model on the lab box

After the Colab notebook produces `cyberrange-sec.gguf`:

## 1. Copy the GGUF to the Ubuntu server
From your machine (adjust the path):
```bash
scp cyberrange-sec.gguf sanjivani@20.0.3.198:~/CyberRange-OS/training/
```

## 2. Create the Ollama model on the box
```bash
cd ~/CyberRange-OS/training
ollama create cyberrange-sec -f Modelfile
ollama run cyberrange-sec "Triage: repeated SQLi signatures from one IP. Reply JSON."
```

## 3. Register it in the platform
In the web app: **Admin -> LLM Registry -> Register Local Model**
- Name: `cyberrange-sec`
- Endpoint: `http://host.docker.internal:11434` (same as your base model)
- Modules: assign the ones you tuned for (e.g. `pentest-copilot`, `soc-copilot`, `assistant`)
- Mark as default if you want it used everywhere.

The platform records the model + prompt version on every call, so graded work
stays reproducible even after you swap models.

## 4. Keep a fallback
Leave the base model (e.g. `llama3.2:3b`) registered too. If a fine-tune
regresses on some task, reassign that module back to the base in one click.

## Re-training later
Add more examples to `training/data/*.jsonl`, re-run `build_dataset.py`, re-run
the notebook, and `ollama create cyberrange-sec` again (it overwrites). Version
your models by name if you want A/B comparison, e.g. `cyberrange-sec-v2`.
