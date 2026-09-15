#!/usr/bin/env python3
"""Flags sqlc queries against a tenant-scoped table that never mention tenant_id.

Law 5.1, quoted verbatim from CLAUDE.md: "Toda query de dados de cliente filtra tenant_id." lang:ok
This is a heuristic, not a proof --
it finds the table a query's FROM/UPDATE/INSERT/DELETE targets and checks whether the literal
text "tenant_id" appears anywhere in the query body (which also matches an aliased column like
`mu.tenant_id`, or a JOIN that brings the column in from another table). It cannot see whether
that mention actually narrows the WHERE clause, and it can't follow a CTE's own table references,
so treat every flagged query as something to read, not something to blindly "fix" by pasting in
a tenant_id column that already isn't doing its job. A query that is genuinely tenant-agnostic
(operator tooling, a lookup keyed by a globally unique token) can be marked
`-- tenant_scope: n/a (reason)` on the line with its `-- name:` header to silence it here.

    python3 scripts/check_tenant_scoping.py           # summary, exit 1 if anything found
    python3 scripts/check_tenant_scoping.py --list     # every flagged query
"""
import argparse
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]
MIGRATIONS = ROOT / "backend" / "internal" / "db" / "migrations"
QUERIES = ROOT / "backend" / "db" / "queries"

CREATE_TABLE = re.compile(r"CREATE TABLE(?: IF NOT EXISTS)?\s+([a-z_]+)\s*\(", re.I)
ALTER_ADD_TENANT = re.compile(r"ALTER TABLE\s+([a-z_]+)\s+ADD COLUMN\s+tenant_id\b", re.I)
QUERY_HEADER = re.compile(r"^--\s*name:\s*(\w+)\s*:(\w+)(.*)$", re.I)
TARGET_TABLE = re.compile(
    r"\b(?:FROM|UPDATE|INTO|JOIN)\s+([a-z_]+)|\bDELETE\s+FROM\s+([a-z_]+)", re.I
)


def tenant_scoped_tables() -> set[str]:
    tables: set[str] = set()
    for f in sorted(MIGRATIONS.glob("*.sql")):
        text = f.read_text(encoding="utf-8", errors="ignore")
        for block_start in (m.start() for m in CREATE_TABLE.finditer(text)):
            # crude but adequate: does tenant_id appear before the matching close paren's
            # statement ends (next blank line or next CREATE/ALTER)? Look at the next 4000 chars.
            m = CREATE_TABLE.match(text, block_start)
            if not m:
                continue
            window = text[block_start : block_start + 4000]
            stmt_end = window.find(";")
            body = window[: stmt_end if stmt_end != -1 else len(window)]
            if re.search(r"\btenant_id\b", body, re.I):
                tables.add(m.group(1).lower())
        for m in ALTER_ADD_TENANT.finditer(text):
            tables.add(m.group(1).lower())
    return tables


def split_queries(text: str) -> list[tuple[str, str, bool, str]]:
    """Returns (name, type, silenced, body) for each `-- name: X :type` block."""
    lines = text.splitlines()
    out = []
    current = None
    for line in lines:
        m = QUERY_HEADER.match(line.strip())
        if m:
            if current:
                out.append(current)
            silenced = "tenant_scope: n/a" in m.group(3)
            current = [m.group(1), m.group(2), silenced, []]
        elif current:
            current[3].append(line)
    if current:
        out.append(current)
    return [(n, t, s, "\n".join(b)) for n, t, s, b in out]


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--list", action="store_true")
    args = ap.parse_args()

    tables = tenant_scoped_tables()
    hits = []
    for f in sorted(QUERIES.glob("*.sql")):
        text = f.read_text(encoding="utf-8", errors="ignore")
        for name, qtype, silenced, body in split_queries(text):
            if silenced:
                continue
            targets = {t.lower() for pair in TARGET_TABLE.findall(body) for t in pair if t}
            scoped_targets = targets & tables
            if scoped_targets and not re.search(r"\btenant_id\b", body, re.I):
                hits.append(f"{f.name}: {name} (:{qtype}) -- touches {', '.join(sorted(scoped_targets))}")

    if args.list:
        for h in hits:
            print(h)
    print(f"\n{len(hits)} quer{'y' if len(hits) == 1 else 'ies'} on a tenant-scoped table with no "
          f"tenant_id mention, out of {len(tables)} tenant-scoped tables found in migrations.")
    if hits:
        print("Run with --list to see them. See law 5.1 and this script's own docstring.")
    return 1 if hits else 0


if __name__ == "__main__":
    sys.exit(main())
