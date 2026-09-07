#!/usr/bin/env python3
"""Validate and merge all training/data/*.jsonl files into dataset.jsonl.

Usage:
    python build_dataset.py            # merges data/*.jsonl -> dataset.jsonl
    python build_dataset.py --check    # validate only, no output file

No external dependencies (standard library only).
"""
import argparse
import glob
import json
import os
import sys

VALID_ROLES = {"system", "user", "assistant"}
HERE = os.path.dirname(os.path.abspath(__file__))
DATA_DIR = os.path.join(HERE, "data")
OUT = os.path.join(HERE, "dataset.jsonl")


def validate_line(obj, where):
    if "messages" not in obj or not isinstance(obj["messages"], list):
        raise ValueError(f"{where}: missing 'messages' list")
    roles = [m.get("role") for m in obj["messages"]]
    if not roles:
        raise ValueError(f"{where}: empty messages")
    for m in obj["messages"]:
        if m.get("role") not in VALID_ROLES:
            raise ValueError(f"{where}: bad role {m.get('role')!r}")
        if not isinstance(m.get("content"), str) or not m["content"].strip():
            raise ValueError(f"{where}: empty content for role {m.get('role')}")
    if "assistant" not in roles:
        raise ValueError(f"{where}: no assistant turn to learn from")
    return True


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--check", action="store_true", help="validate only")
    args = ap.parse_args()

    files = sorted(glob.glob(os.path.join(DATA_DIR, "*.jsonl")))
    if not files:
        print(f"No .jsonl files found in {DATA_DIR}")
        sys.exit(1)

    total, kept = 0, 0
    out_lines = []
    for path in files:
        with open(path, "r", encoding="utf-8") as f:
            for i, line in enumerate(f, 1):
                line = line.strip()
                if not line:
                    continue
                total += 1
                where = f"{os.path.basename(path)}:{i}"
                try:
                    obj = json.loads(line)
                    validate_line(obj, where)
                except Exception as e:  # noqa: BLE001
                    print(f"  SKIP {where}: {e}")
                    continue
                kept += 1
                out_lines.append(json.dumps(obj, ensure_ascii=False))

    print(f"Validated {kept}/{total} examples from {len(files)} file(s).")
    if args.check:
        return
    with open(OUT, "w", encoding="utf-8") as f:
        f.write("\n".join(out_lines) + "\n")
    print(f"Wrote {kept} examples -> {OUT}")
    if kept < 200:
        print("NOTE: fewer than 200 examples; add more for a useful fine-tune.")


if __name__ == "__main__":
    main()
