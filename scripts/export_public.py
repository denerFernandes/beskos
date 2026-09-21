#!/usr/bin/env python3
"""Export the tracked tree into a fresh repository for the public edition.

The public repository is born from a single commit: this script copies every
tracked file except the ones listed in EXCLUDE (operator console, production
deployment, internal documents) into a target directory, regenerates database
code with sqlc v1.27.0, and reports remaining publication blockers.

    python3 scripts/export_public.py /path/to/export        # copy + residue report
    python3 scripts/export_public.py --check                # temporary export + report
    python3 scripts/export_public.py --check --private-only  # private edition boundary
"""
import argparse
import fnmatch
import pathlib
import re
import shutil
import subprocess
import sys
import tempfile

ROOT = pathlib.Path(__file__).resolve().parents[1]

# Paths that never leave the private repository. Globs match the whole path.
EXCLUDE = [
    # Operator console and its API (beskos-cloud).
    "manager/*",
    "backend/cmd/manager-api/*",
    "backend/internal/managerapi/*",
    "backend/cmd/ses-tenants/*",
    "backend/cmd/stalwart-sieve/*",
    "backend/cmd/worker/reputation.go",
    "backend/cmd/worker/reputation_test.go",
    "backend/internal/ses/*",
    "backend/internal/stalwart/sieve.go",
    "backend/internal/httpapi/hosted_routes.go",
    "backend/internal/httpapi/hosted_routes_test.go",
    "backend/internal/httpapi/ses_webhook.go",
    "backend/internal/httpapi/waitlist_public.go",
    "backend/internal/httpapi/contact_public.go",
    "backend/internal/notify/marketing.go",
    "backend/internal/provisioning/relay_test.go",
    "backend/internal/provisioning/zone_relay.go",
    "backend/internal/provisioning/zone_ses_test.go",
    "backend/db/queries/queries_waitlist.sql",
    "backend/internal/db/queries_waitlist.sql.go",
    "backend/internal/db/migrations/00140_waitlist.sql",
    "backend/db/queries/queries_delivery.sql",
    "backend/internal/db/queries_delivery.sql.go",
    "backend/internal/db/migrations/00107_mail_events.sql",
    "backend/cmd/worker/metering.go",
    "backend/cmd/worker/metering_test.go",
    "backend/db/queries/queries_usage.sql",
    "backend/internal/db/queries_usage.sql.go",
    "backend/internal/db/migrations/00102_tenant_daily_usage.sql",
    # Hosted checkout, tax calculation, and self-service onboarding. The implementation itself
    # lives in backend/ee (the -tags pro boundary, see backend/ee/README.md) and ships in full;
    # only the pricing-tier data below stays private.
    "backend/internal/db/migrations/00118_pricing_tiers.sql",
    "backend/internal/db/migrations/00119_pro_eur_align.sql",
    "frontend/src/pages/Signup.tsx",
    "frontend/src/pages/signup.css",
    "frontend/src/panel/BillingSection.tsx",
    "frontend/src/panel/SubscribeWizard.tsx",
    # Manager-only tables and queries. Generated models are rebuilt after the copy.
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
    # Agent configuration and the landing gitlink (separate repository). CLAUDE.md itself ships --
    # CONTRIBUTING.md links to it as the canonical rules document, and it's plain English prose,
    # not agent-only config.
    ".claude/*",
    ".codex/*",
    "AGENTS.md",
    "landing",
    "scripts/export_public.py",
    "scripts/test_export_public.py",
    # Internal review tooling: house-style conventions (English-only, file size, formal address)
    # specific to how this team works, not a public contribution requirement. CONTRIBUTING.md and
    # the public CI workflow are adjusted accordingly (see the Makefile/ci.yml overrides below).
    "scripts/check_laws.py",
    "scripts/check_docs.py",
    "scripts/check_register.py",
    "scripts/comment_audit.py",
    "scripts/check_tenant_scoping.py",
    "scripts/gen_env_docs.py",
    "scripts/public/*",
    ".github/workflows/public-export.yml",
]

# Published path -> source path. The source replaces whatever tracked_files() would have put at
# the published path (its own tracked copy there, if any, is excluded below) -- used for files
# that need a different version in public than the one the team uses day to day.
OVERRIDES = {
    # docs/README-oss.md becomes the public repo's root README.md, separate from the internal
    # README.md (which documents day-to-day status for the team) -- its links are written relative
    # to the repo root it ships at, not to docs/ where the source file lives.
    "README.md": "docs/README-oss.md",
    "deploy/docker-compose.yml": "deploy/docker-compose.community.yml",
    "backend/Dockerfile": "scripts/public/Dockerfile",
    "Makefile": "scripts/public/Makefile",
    ".github/workflows/ci.yml": "scripts/public/ci.yml",
    "backend/cmd/worker/edition_workers.go": "scripts/public/edition_workers.go",
    "backend/internal/billing/catalog.go": "scripts/public/billing_catalog.go",
    "backend/internal/billing/plans_test.go": "scripts/public/billing_catalog_test.go",
    "backend/internal/db/migrations/00101_plans.sql": "scripts/public/plans.sql",
    "frontend/src/edition.ts": "scripts/public/frontend_edition.ts",
    "backend/internal/httpapi/hosted_services.go": "scripts/public/hosted_services.go",
    "backend/cmd/api/hosted_services.go": "scripts/public/api_hosted_services.go",
    "backend/internal/provisioning/relay_config.go": "scripts/public/provisioning_relay.go",
}

# Residue checks: things the exported tree must not contain. Each entry is a
# regex applied to text files, with the paths where a match is acceptable.
RESIDUE = [
    ("manager console reference", re.compile(r"managerapi|manager-api|manager_[a-z]+|\bManager[A-Z]\w*|docs/manager\.md"), []),
    ("cloud metering reference", re.compile(r"tenant_daily_usage|TenantDailyUsage|RollupTenantDailyUsage|ExpiredRecordings|DeleteRecordingRow|startMetering"), []),
    ("hosted billing implementation", re.compile(r"\bStripeProvider\b|\bNewStripe\b|func (?:Checkout|BillingWebhook|Signup)\(|export function (?:SignupPage|SubscribeWizard|BillingSection)\b"), ["backend/ee/*"]),
    ("hosted marketing and relay implementation", re.compile(
        r'"github.com/beskosorg/beskos/backend/internal/ses"|\bAugmentZoneForSES\b|'
        r'func .*handle(?:JoinWaitlist|ContactForm|SESWebhook)\b|\btype (?:Waitlist|MailEvent) struct|'
        r'CREATE TABLE (?:waitlist|mail_events)\b'), []),
    ("hardcoded brand domain", re.compile(r"beskos\.com"), ["*_test.go", "*.test.ts", "deploy/*", "docs/*", "README.md"]),
    ("personal email", re.compile(r"[a-z0-9._-]+@(gmail|hotmail|mybeevo)\.[a-z]+"), []),
    ("comment in Portuguese", re.compile(r"(//|#|\*)\s.*\b(não|também|já|ficheiro|utilizador|usuário|porque)\b"), ["*.md", "backend/internal/db/*", "*.json"]),  # lang:ok
    ("dated comment", re.compile(r"(//|/\*|\*|#)\s.*\b\d{2}/\d{2}/20\d{2}\b"), ["*.md"]),
    ("internal phase reference", re.compile(r"(//|/\*|\*)\s.*\b(M[0-9]|A[0-9]b?|Fase [0-9])\b"), ["*.md"]),
]

TEXT_SUFFIXES = {".go", ".ts", ".tsx", ".js", ".mjs", ".rs", ".py", ".sh", ".sql", ".yml", ".yaml",
                 ".json", ".toml", ".md", ".css", ".html", ".conf", ".txt", ".env", ".example"}
# Extension-less files that still carry residue-worthy text (a build recipe listing binaries, a
# reverse-proxy config): a bare filename check, since Path.suffix is empty for all of these and
# they would otherwise never be scanned.
TEXT_BASENAMES = {"Dockerfile", "Makefile", "Caddyfile"}


def is_text_file(p: pathlib.Path) -> bool:
    return p.suffix in TEXT_SUFFIXES or p.name in TEXT_BASENAMES or p.name.startswith("Dockerfile.")


def tracked_files() -> list[str]:
    out = subprocess.run(["git", "ls-files"], cwd=ROOT, capture_output=True, text=True, check=True).stdout
    return [line for line in out.splitlines() if line]


def excluded(path: str) -> bool:
    if path in OVERRIDES:
        return True  # replaced below, not copied as tracked
    return any(fnmatch.fnmatch(path, pattern) or path == pattern.rstrip("/*") for pattern in EXCLUDE)


def residue(paths: list[str], root: pathlib.Path = ROOT) -> dict[str, list[str]]:
    """Check final exported files, after overrides and database generation."""
    hits: dict[str, list[str]] = {name: [] for name, _, _ in RESIDUE}
    entries = [(rel, root / rel) for rel in paths]
    for rel, p in entries:
        if not is_text_file(p) or not p.is_file():
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


def regenerate_database(target: pathlib.Path) -> None:
    """Generate only the database types and queries present in the public schema."""
    config = target / "backend/sqlc.yaml"
    text = config.read_text()
    text = re.sub(
        r'          # manager[^\n]*\n'
        r'          - column: "manager_[^"\n]+"\n'
        r'            go_type: "[^"\n]+"\n',
        '', text,
    )
    config.write_text(text)
    subprocess.run(["sqlc", "generate"], cwd=target / "backend", check=True)


def prepare_export(target: pathlib.Path) -> None:
    regenerate_database(target)
    # Runs THIS repo's own copy of the script (gen_env_docs.py doesn't ship publicly, see
    # scripts/public/ below), pointed at the exported tree so docs/configuration.md still comes
    # out fresh for what actually ships.
    subprocess.run([sys.executable, str(ROOT / "scripts" / "gen_env_docs.py"), str(target)], check=True)


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
    prepare_export(target)


def report_tree(target: pathlib.Path, manager_only: bool) -> int:
    paths = [p.relative_to(target).as_posix() for p in target.rglob("*") if p.is_file()]
    report = residue(paths, target)
    if manager_only:
        report = {name: report[name] for name in (
            "manager console reference", "cloud metering reference", "hosted billing implementation",
            "hosted marketing and relay implementation",
        )}
    total = sum(len(v) for v in report.values())
    for name, items in report.items():
        print(f"\n{name}: {len(items)} file(s)")
        for item in items:
            print(f"  {item}")
    scope = "Private edition boundary" if manager_only else "publication residue"
    print(f"\n{scope}: {total} file finding(s)")
    return 1 if total else 0


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("target", nargs="?", help="directory to export into")
    ap.add_argument("--check", action="store_true", help="only print the residue report")
    ap.add_argument("--private-only", "--manager-only", dest="manager_only", action="store_true",
                    help="check only Manager, cloud metering, and hosted billing")
    args = ap.parse_args()
    if not args.target and not args.check:
        ap.error("give a target directory or --check")

    files = tracked_files()
    kept = [f for f in files if not excluded(f)]
    dropped = len(files) - len(kept)
    print(f"tracked {len(files)}, excluded {dropped}, exported {len(kept)}")

    if args.target:
        target = pathlib.Path(args.target).resolve()
        copy_tree(kept, target)
        print(f"copied into {args.target}")
        return report_tree(target, args.manager_only)

    with tempfile.TemporaryDirectory(prefix="beskos-public-") as directory:
        target = pathlib.Path(directory)
        copy_tree(kept, target)
        return report_tree(target, args.manager_only)


if __name__ == "__main__":
    sys.exit(main())
