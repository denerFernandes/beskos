#!/usr/bin/env python3
"""Export the tracked tree into a fresh repository for the public edition.

The public repository is born from a single commit: this script copies every
tracked file except the ones listed in EXCLUDE (operator console, production
deployment, internal documents) into a target directory, and reports what still
has to be cleaned by hand before the export is publishable.

    python3 scripts/export_public.py /path/to/export        # copy + residue report
    python3 scripts/export_public.py --check                # residue report only
"""
import argparse
import fnmatch
import pathlib
import re
import shutil
import subprocess
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]

# Paths that never leave the private repository. Globs match the whole path.
EXCLUDE = [
    # Operator console and its API (beskos-cloud).
    "manager/*",
    "backend/cmd/manager-api/*",
    "backend/internal/managerapi/*",
    "backend/cmd/ses-tenants/*",
    # Manager-only tables and queries (schema, seed data, and generated code for a console
    # that isn't in this export; its Go structs stay in models.go, unused, until the next
    # sqlc regeneration against a trimmed schema).
    "backend/internal/db/migrations/00100_manager.sql",
    "backend/internal/db/migrations/00103_manager_settings.sql",
    "backend/internal/db/migrations/00106_manager_admin.sql",
    "backend/internal/db/migrations/00108_manager_servers.sql",
    "backend/db/queries/queries_manager.sql",
    "backend/internal/db/queries_manager.sql.go",
    # Production deployment of our own SaaS.
    "deploy/deploy.py",
    "deploy/setenv.py",
    "deploy/provision/*",
    "deploy/status/*",
    "deploy/stalwart/dns-beskos.zone",
    ".github/workflows/deploy.yml",
    # Internal documents: brief, roadmap, evaluations, this plan.
    "project.md",
    "docs/manager.md",
    "docs/AVALIACAO-PLATAFORMA-PRODUCAO.md",  # lang:ok
    "docs/ROADMAP.md",
    "docs/ROADMAP-CRM.md",
    "docs/FUNCIONALIDADES.md",
    "docs/GAPS-FUNCIONALIDADES.md",
    "docs/landing-europa.md",
    "docs/OPEN-SOURCE.md",
    "docs/ASSISTENTE-IA.md",
    "docs/DEPLOY.md",  # our own production runbook (CapRover topology, manager-api placement)
    "docs/DNS-PAGE.md",  # dated planning note; the feature itself is covered in FUNCTIONALITY.md
    "docs/cifra-ficheiros.md",  # superseded by docs/adr/0004-aes-gcm-block-file-encryption.md -- lang:ok
    "docs/wiki-features.md",  # dated design draft; the feature itself is covered in FUNCTIONALITY.md
    "deploy/backup/MIGRACAO-STALWART-POSTGRES.md",  # dated post-mortem, superseded by DIVERGENCES.md D9 -- lang:ok
    "deploy/DIVERGENCES.md",  # detailed internal reverse-engineering notes and production topology
    "docs/README-oss.md",  # becomes README.md via OVERRIDES, not kept at its own path too
    "deploy/docker-compose.community.yml",  # becomes deploy/docker-compose.yml via OVERRIDES
    # Agent configuration and the landing gitlink (separate repository).
    ".claude/*",
    ".codex/*",
    "AGENTS.md",
    "CLAUDE.md",
    "landing",
]

# Published path -> source path. The source replaces whatever tracked_files() would have put at
# the published path (its own tracked copy there, if any, is excluded below) -- used for files
# that need a different version in public than the one the team uses day to day.
OVERRIDES = {
    "README.md": "docs/README-oss.md",
    "deploy/docker-compose.yml": "deploy/docker-compose.community.yml",
}

# Residue checks: things the exported tree must not contain. Each entry is a
# regex applied to text files, with the paths where a match is acceptable.
RESIDUE = [
    ("manager console reference", re.compile(r"managerapi|manager-api|manager_users|docs/manager\.md"), []),
    ("hardcoded brand domain", re.compile(r"beskos\.com"), ["*_test.go", "*.test.ts", "deploy/*", "docs/*", "README.md"]),
    ("personal email", re.compile(r"[a-z0-9._-]+@(gmail|hotmail|mybeevo)\.[a-z]+"), []),
    ("comment in Portuguese", re.compile(r"(//|#|\*)\s.*\b(não|também|já|ficheiro|utilizador|usuário|porque)\b"), ["*.md", "backend/internal/db/*", "*.json"]),  # lang:ok
    ("dated comment", re.compile(r"(//|/\*|\*|#)\s.*\b\d{2}/\d{2}/20\d{2}\b"), ["*.md"]),
    ("internal phase reference", re.compile(r"(//|/\*|\*)\s.*\b(M[0-9]|A[0-9]b?|Fase [0-9])\b"), ["*.md"]),
]

TEXT_SUFFIXES = {".go", ".ts", ".tsx", ".js", ".mjs", ".rs", ".py", ".sh", ".sql", ".yml", ".yaml",
                 ".json", ".toml", ".md", ".css", ".html", ".conf", ".txt", ".env", ".example"}


def tracked_files() -> list[str]:
    out = subprocess.run(["git", "ls-files"], cwd=ROOT, capture_output=True, text=True, check=True).stdout
    return [line for line in out.splitlines() if line]


def excluded(path: str) -> bool:
    if path in OVERRIDES:
        return True  # replaced below, not copied as tracked
    return any(fnmatch.fnmatch(path, pattern) or path == pattern.rstrip("/*") for pattern in EXCLUDE)


def residue(paths: list[str]) -> dict[str, list[str]]:
    """Checks the tree as it will actually be published: kept files at their own path, plus each
    override's source content checked at its PUBLISHED path (so e.g. README.md's allowances
    apply to docs/README-oss.md's content, not to docs/*'s)."""
    hits: dict[str, list[str]] = {name: [] for name, _, _ in RESIDUE}
    entries = [(rel, ROOT / rel) for rel in paths]
    entries += [(pub, ROOT / src) for pub, src in OVERRIDES.items()]
    for rel, p in entries:
        if p.suffix not in TEXT_SUFFIXES or not p.is_file():
            continue
        try:
            text = p.read_text(encoding="utf-8")
        except (UnicodeDecodeError, OSError):
            continue
        for name, rx, allowed in RESIDUE:
            if any(fnmatch.fnmatch(rel, a) for a in allowed):
                continue
            n = sum(1 for line in text.splitlines() if rx.search(line))
            if n:
                hits[name].append(f"{rel} ({n})")
    return hits


def copy_tree(paths: list[str], target: pathlib.Path) -> None:
    if target.exists() and any(target.iterdir()):
        sys.exit(f"target {target} is not empty")
    for rel in paths:
        src = ROOT / rel
        if not src.is_file():
            continue
        dst = target / rel
        dst.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(src, dst)
    for pub, src_rel in OVERRIDES.items():
        dst = target / pub
        dst.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(ROOT / src_rel, dst)


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("target", nargs="?", help="directory to export into")
    ap.add_argument("--check", action="store_true", help="only print the residue report")
    args = ap.parse_args()
    if not args.target and not args.check:
        ap.error("give a target directory or --check")

    files = tracked_files()
    kept = [f for f in files if not excluded(f)]
    dropped = len(files) - len(kept)
    print(f"tracked {len(files)}, excluded {dropped}, exported {len(kept)}")

    if args.target:
        copy_tree(kept, pathlib.Path(args.target).resolve())
        print(f"copied into {args.target}")

    report = residue(kept)
    total = sum(len(v) for v in report.values())
    for name, items in report.items():
        print(f"\n{name}: {len(items)} file(s)")
        for item in items[:8]:
            print(f"  {item}")
        if len(items) > 8:
            print(f"  ... and {len(items) - 8} more")
    print(f"\nresidue: {total} file(s) still need cleaning before publishing")
    return 1 if total else 0


if __name__ == "__main__":
    sys.exit(main())
