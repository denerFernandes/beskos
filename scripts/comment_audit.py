#!/usr/bin/env python3
"""Flags comments that read like a work diary instead of documentation.

A comment that names a date, a sprint/phase, or an internal planning document ties the code to
a conversation nobody outside the team can see, and it rots the moment that document moves --
see docs/OPEN-SOURCE.md §5 for the policy this enforces. This script does not check language
(scripts/check_laws.py already does, on new lines only); it checks EVERY comment in the tree,
because a comment written months ago is exactly as confusing to a new reader as one written
today. It is a reporting tool, not (yet) a CI gate: run it, read the count, and chip away at it.

    python3 scripts/comment_audit.py               # summary, exit 1 if anything found
    python3 scripts/comment_audit.py --list         # every match, file:line
    python3 scripts/comment_audit.py --paths backend/internal/billing
"""
import argparse
import fnmatch
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]

EXTS = {".go": "//", ".ts": "//", ".tsx": "//", ".rs": "//"}

SKIP_DIRS = {"node_modules", "dist", "target", ".git", "internal/db", "src-tauri/target"}

RULES = [
    ("dated", re.compile(r"\b\d{1,2}/\d{1,2}/20\d{2}\b")),
    ("internal-doc-ref", re.compile(r"\b(docs/manager\.md|project\.md|DIVERGENCES\.md)\b")),
    ("phase-ref", re.compile(r"\b(M[0-9]|A[0-9]b?|Fase [0-9]+|§[0-9])\b")),
    ("session-narrative", re.compile(r"\b(sessão|decisão desta sessão|escrita a)\b", re.I)),  # lang:ok
]


def iter_files(paths: list[str]):
    for base in paths:
        p = ROOT / base
        if p.is_file():
            yield p
            continue
        for f in p.rglob("*"):
            if not f.is_file() or f.suffix not in EXTS:
                continue
            if any(part in SKIP_DIRS for part in f.relative_to(ROOT).parts):
                continue
            yield f


def comment_lines(path: pathlib.Path):
    marker = EXTS[path.suffix]
    try:
        text = path.read_text(encoding="utf-8")
    except (UnicodeDecodeError, OSError):
        return
    for i, line in enumerate(text.splitlines(), 1):
        stripped = line.strip()
        if stripped.startswith(marker) or stripped.startswith("* ") or stripped.startswith("/*"):
            yield i, line


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--paths", nargs="+", default=["backend", "frontend/src", "sync-daemon/src", "frontend/src-tauri/src"])
    ap.add_argument("--list", action="store_true", help="print every match instead of just the summary")
    args = ap.parse_args()

    counts = {name: 0 for name, _ in RULES}
    hits: list[str] = []
    for f in iter_files(args.paths):
        for lineno, line in comment_lines(f):
            for name, rx in RULES:
                if rx.search(line):
                    counts[name] += 1
                    rel = f.relative_to(ROOT)
                    hits.append(f"{rel}:{lineno}: [{name}] {line.strip()[:120]}")

    if args.list:
        for h in hits:
            print(h)

    total = sum(counts.values())
    print(f"\n{total} flagged comment(s):")
    for name, n in counts.items():
        print(f"  {name}: {n}")
    if total:
        print("\nRun with --list to see every one. See docs/OPEN-SOURCE.md §5 for the policy.")
    return 1 if total else 0


if __name__ == "__main__":
    sys.exit(main())
