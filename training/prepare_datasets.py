#!/usr/bin/env python3
"""Download public cybersecurity datasets and convert them into the chat-JSONL
format used by finetune_qlora.ipynb.

It pulls from several independent public sources. Any source that is
unavailable is skipped with a warning, so the script still produces a usable
dataset from whatever succeeded. It also generates high-quality MITRE ATT&CK
tagging examples directly from the official ATT&CK STIX bundle (real technique
descriptions -> real technique IDs), which needs no third-party dataset at all.

Usage:
    pip install -r requirements.txt
    python prepare_datasets.py                 # all sources, default caps
    python prepare_datasets.py --max-per-source 500
    python prepare_datasets.py --only mitre_stix,primus
    python prepare_datasets.py --list          # show available sources

Output:
    dataset.jsonl        (train split)
    dataset_eval.jsonl   (held-out eval split, decontaminated)
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
import random
import re
import sys
import urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
OUT_TRAIN = os.path.join(HERE, "dataset.jsonl")
OUT_EVAL = os.path.join(HERE, "dataset_eval.jsonl")
LOCAL_DATA_DIR = os.path.join(HERE, "data")

# Official MITRE ATT&CK Enterprise STIX bundle (public, CC-BY 4.0 with attribution).
ATTACK_STIX_URL = (
    "https://raw.githubusercontent.com/mitre-attack/attack-stix-data/master/"
    "enterprise-attack/enterprise-attack.json"
)

SYS_MITRE = "You classify security activity to one MITRE ATT&CK technique. Reply only with JSON."
SYS_SOC = "You are the Blue Team SOC Copilot. Triage the alert and reply only with JSON. Advisory only."
SYS_QA = (
    "You are a cybersecurity teaching assistant for a college lab. "
    "Answer accurately and concisely."
)


# --------------------------------------------------------------------------- utils
def log(msg: str) -> None:
    print(msg, flush=True)


def norm(text: str) -> str:
    return re.sub(r"\s+", " ", (text or "")).strip()


def clean_md(text: str) -> str:
    """Strip ATT&CK markdown citation noise like (Citation: ...) and links."""
    text = re.sub(r"\(Citation:[^)]*\)", "", text or "")
    text = re.sub(r"\[([^\]]+)\]\([^)]*\)", r"\1", text)
    return norm(text)


def example(system: str, user: str, assistant: str) -> dict:
    return {
        "messages": [
            {"role": "system", "content": system},
            {"role": "user", "content": user},
            {"role": "assistant", "content": assistant},
        ]
    }


def truncate(text: str, limit: int) -> str:
    text = norm(text)
    return text if len(text) <= limit else text[:limit].rsplit(" ", 1)[0] + "..."


def try_load(repo: str, **kw):
    """Load a Hugging Face dataset, returning None on any failure."""
    try:
        from datasets import load_dataset
    except ImportError:
        log("  ! 'datasets' not installed - run: pip install -r requirements.txt")
        return None
    try:
        return load_dataset(repo, **kw)
    except Exception as e:  # noqa: BLE001
        log(f"  ! skipping {repo}: {type(e).__name__}: {e}")
        return None


def first_field(row: dict, names: list[str]) -> str | None:
    """Return the first present, non-empty string field from candidates."""
    for n in names:
        v = row.get(n)
        if isinstance(v, str) and v.strip():
            return v
    return None


# ----------------------------------------------------------------- source: ATT&CK
def src_mitre_stix(cap: int) -> list[dict]:
    """Generate MITRE tagging + explanation examples from the official STIX bundle.

    This is the highest-signal source for your platform because the labels are
    authoritative and the phrasing matches real technique descriptions.
    """
    cache = os.path.join(HERE, "enterprise-attack.json")
    if not os.path.exists(cache):
        log(f"  downloading ATT&CK STIX bundle -> {os.path.basename(cache)}")
        try:
            urllib.request.urlretrieve(ATTACK_STIX_URL, cache)
        except Exception as e:  # noqa: BLE001
            log(f"  ! could not download ATT&CK bundle: {e}")
            return []
    try:
        with open(cache, "r", encoding="utf-8") as f:
            bundle = json.load(f)
    except Exception as e:  # noqa: BLE001
        log(f"  ! could not parse ATT&CK bundle: {e}")
        return []

    out: list[dict] = []
    for obj in bundle.get("objects", []):
        if obj.get("type") != "attack-pattern":
            continue
        if obj.get("revoked") or obj.get("x_mitre_deprecated"):
            continue
        tid = ""
        for ref in obj.get("external_references", []):
            if ref.get("source_name") == "mitre-attack" and ref.get("external_id"):
                tid = ref["external_id"]
                break
        if not tid:
            continue
        name = norm(obj.get("name", ""))
        desc = clean_md(obj.get("description", ""))
        if not name or len(desc) < 60:
            continue
        tactics = [
            norm(p.get("phase_name", ""))
            for p in obj.get("kill_chain_phases", [])
            if p.get("kill_chain_name") == "mitre-attack"
        ]
        tactic = tactics[0] if tactics else ""

        # (a) description -> technique id  (the core tagging task)
        out.append(
            example(
                SYS_MITRE,
                f"Activity observed:\n{truncate(desc, 700)}",
                json.dumps(
                    {
                        "technique_id": tid,
                        "technique_name": name,
                        "tactic": tactic,
                        "confidence": 0.9,
                    },
                    ensure_ascii=False,
                ),
            )
        )
        # (b) technique id -> explanation (teaches domain knowledge + tone)
        out.append(
            example(
                SYS_QA,
                f"Explain MITRE ATT&CK technique {tid} ({name}) briefly.",
                f"{tid} - {name}"
                + (f" falls under the {tactic} tactic. " if tactic else ". ")
                + truncate(desc, 500),
            )
        )
        # (c) detection guidance, when ATT&CK provides it
        detection = clean_md(obj.get("x_mitre_detection", ""))
        if len(detection) > 80:
            out.append(
                example(
                    SYS_QA,
                    f"How would a SOC analyst detect {tid} ({name})?",
                    truncate(detection, 600),
                )
            )

    random.shuffle(out)
    return out[:cap]


# --------------------------------------------------------- source: HF instruction sets
def src_primus(cap: int) -> list[dict]:
    """Trend Micro Primus-Instruct: ~1k curated cybersecurity QA pairs."""
    ds = try_load("trendmicro-ailab/Primus-Instruct", split="train")
    if ds is None:
        return []
    out = []
    for row in ds:
        q = first_field(row, ["instruction", "question", "input", "prompt"])
        a = first_field(row, ["output", "response", "answer", "completion"])
        if not q or not a:
            continue
        out.append(example(SYS_QA, norm(q), truncate(a, 1200)))
        if len(out) >= cap:
            break
    return out


def src_attack_scenarios(cap: int) -> list[dict]:
    """Security scenario -> MITRE technique reasoning pairs."""
    out: list[dict] = []
    for repo in ("dattaraj/security-attacks-MITRE", "medmac01/mitre-data-lines"):
        ds = try_load(repo, split="train")
        if ds is None:
            continue
        for row in ds:
            # These sets are commonly single-text or prompt/response shaped.
            q = first_field(row, ["prompt", "instruction", "question", "input", "Scenario"])
            a = first_field(row, ["response", "output", "answer", "completion", "Response"])
            if not q or not a:
                raw = first_field(row, ["text", "content"])
                if not raw or "MITRE" not in raw:
                    continue
                # Split a combined "scenario ... aligns with MITRE ..." record.
                parts = re.split(r"(?=This scenario aligns with)", raw, maxsplit=1)
                if len(parts) != 2:
                    continue
                q, a = parts[0], parts[1]
            out.append(example(SYS_MITRE, truncate(q, 700), truncate(a, 700)))
            if len(out) >= cap:
                return out
    return out


def src_cve_techniques(cap: int) -> list[dict]:
    """CIRCL gold set: CVE description -> expert ATT&CK technique labels."""
    ds = try_load("CIRCL/vulnerability-attack-techniques", split="train")
    if ds is None:
        return []
    out = []
    for row in ds:
        desc = first_field(row, ["description", "cve_description", "text"])
        labels = row.get("techniques") or row.get("labels") or row.get("attack_techniques")
        if not desc or not labels:
            continue
        if isinstance(labels, str):
            labels = [labels]
        ids = [str(x) for x in labels if str(x).strip()]
        if not ids:
            continue
        out.append(
            example(
                SYS_MITRE,
                f"Vulnerability description:\n{truncate(desc, 700)}",
                json.dumps({"technique_ids": ids, "confidence": 0.85}, ensure_ascii=False),
            )
        )
        if len(out) >= cap:
            break
    return out


def src_local(cap: int) -> list[dict]:
    """Your own hand-written examples in training/data/*.jsonl (highest priority)."""
    import glob

    out = []
    for path in sorted(glob.glob(os.path.join(LOCAL_DATA_DIR, "*.jsonl"))):
        with open(path, "r", encoding="utf-8") as f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                try:
                    obj = json.loads(line)
                    if isinstance(obj.get("messages"), list) and obj["messages"]:
                        out.append(obj)
                except Exception:  # noqa: BLE001
                    continue
        if len(out) >= cap:
            break
    return out[:cap]


SOURCES = {
    "local": (src_local, "Your own examples in training/data/*.jsonl"),
    "mitre_stix": (src_mitre_stix, "Official MITRE ATT&CK STIX -> tagging/explain/detect"),
    "primus": (src_primus, "trendmicro-ailab/Primus-Instruct cybersecurity QA"),
    "attack_scenarios": (src_attack_scenarios, "Scenario -> MITRE technique reasoning"),
    "cve_techniques": (src_cve_techniques, "CIRCL CVE -> ATT&CK technique labels"),
}


# ------------------------------------------------------------------------- main
def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--max-per-source", type=int, default=1500)
    ap.add_argument("--only", type=str, default="", help="comma-separated source names")
    ap.add_argument("--eval-frac", type=float, default=0.05)
    ap.add_argument("--seed", type=int, default=42)
    ap.add_argument("--list", action="store_true")
    args = ap.parse_args()

    if args.list:
        for name, (_, desc) in SOURCES.items():
            print(f"  {name:18s} {desc}")
        return

    random.seed(args.seed)
    selected = [s.strip() for s in args.only.split(",") if s.strip()] or list(SOURCES)

    collected: list[dict] = []
    for name in selected:
        if name not in SOURCES:
            log(f"! unknown source: {name}")
            continue
        fn, desc = SOURCES[name]
        log(f"[{name}] {desc}")
        try:
            rows = fn(args.max_per_source)
        except Exception as e:  # noqa: BLE001
            log(f"  ! source failed: {type(e).__name__}: {e}")
            rows = []
        log(f"  -> {len(rows)} examples")
        collected.extend(rows)

    if not collected:
        log("\nNo examples collected. Check your network, or run with --only mitre_stix")
        sys.exit(1)

    # Deduplicate on the assistant target + user prompt.
    seen: set[str] = set()
    unique: list[dict] = []
    for ex in collected:
        msgs = ex.get("messages", [])
        key_src = "".join(m.get("content", "") for m in msgs if m.get("role") != "system")
        key = hashlib.sha256(norm(key_src).lower().encode("utf-8")).hexdigest()
        if key in seen:
            continue
        seen.add(key)
        unique.append(ex)

    random.shuffle(unique)
    n_eval = max(1, int(len(unique) * args.eval_frac)) if len(unique) > 40 else 0
    eval_rows, train_rows = unique[:n_eval], unique[n_eval:]

    with open(OUT_TRAIN, "w", encoding="utf-8") as f:
        for ex in train_rows:
            f.write(json.dumps(ex, ensure_ascii=False) + "\n")
    if eval_rows:
        with open(OUT_EVAL, "w", encoding="utf-8") as f:
            for ex in eval_rows:
                f.write(json.dumps(ex, ensure_ascii=False) + "\n")

    log("")
    log(f"collected : {len(collected)}")
    log(f"unique    : {len(unique)}  (deduplicated)")
    log(f"train     : {len(train_rows)} -> {os.path.basename(OUT_TRAIN)}")
    log(f"eval      : {len(eval_rows)} -> {os.path.basename(OUT_EVAL)}")
    log("")
    log("Next: upload dataset.jsonl to the Colab notebook (finetune_qlora.ipynb).")


if __name__ == "__main__":
    main()
