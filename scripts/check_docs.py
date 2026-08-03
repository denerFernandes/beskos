#!/usr/bin/env python3
"""Does the documentation still tell the truth?

Documentation is the part of a product that rots fastest, and it rots silently: the day somebody
changes a recovery window from 30 days to 14, nothing reminds them that a sentence in a file they
have never opened now promises the old number. The customer finds out by following it.

THE FIRST VERSION OF THIS SCRIPT DID NOT WORK, and the way it failed is worth keeping. It searched
the articles for the string "30 days". Change the number in one sentence and the check still passed,
because "30 days" appeared in another. A verification that looks for a string it already expects
finds only what it already knew.

So the articles no longer contain numbers at all: they write {recoveryDays} and the value is
substituted at render, from lib/docs-facts.ts. This script has one job left, which it can do
honestly -- compare that single table against the constants in the backend. Eight translations
cannot disagree with each other, because none of them holds a number.

Run with `make check`. Exits non-zero with the disagreement spelled out.
"""

import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]
FACTS = ROOT / "landing" / "lib" / "docs-facts.ts"


def read(rel: str) -> str:
    p = ROOT / rel
    return p.read_text(encoding="utf-8", errors="replace") if p.exists() else ""


def fact(name: str) -> str | None:
    m = re.search(rf'{name}: "([^"]*)"', FACTS.read_text(encoding="utf-8"))
    return m.group(1) if m else None


def code(rel: str, pattern: str) -> str | None:
    m = re.search(pattern, read(rel))
    return m.group(1) if m else None


# Each row: the fact the docs state, and where the product decides it.
PINS = [
    ("recoveryDays", "backend/internal/httpapi/mailbox_lifecycle.go",
     r"RecoveryWindow = (\d+) \* 24 \* time\.Hour", "RecoveryWindow"),
    ("graceDays", "backend/internal/billing/account_state.go",
     r"GracePeriod = (\d+) \* 24 \* time\.Hour", "GracePeriod"),
    ("warnDaysBefore", "backend/internal/billing/account_state.go",
     r"EndingSoon = (\d+) \* 24 \* time\.Hour", "EndingSoon"),
    ("trialDays", "backend/internal/httpapi/signup.go",
     r'atoiOr\(os\.Getenv\("BILLING_TRIAL_DAYS"\), (\d+)\)', "BILLING_TRIAL_DAYS default"),
    ("backupDays", "deploy/backup/backup.sh", r"RETENTION_DAYS:=(\d+)", "RETENTION_DAYS"),
]

# Plan limits, straight from the migration that seeds the catalogue.
TIERS = "backend/internal/db/migrations/00118_pricing_tiers.sql"
PLANS = [
    ("base", "baseSeats", "baseStorage", "baseParticipants", "baseSend"),
    ("business", "businessSeats", "businessStorage", "businessParticipants", "businessSend"),
    ("pro", "proSeats", "proStorage", "proParticipants", "proSend"),
]


def plan_row(sql: str, key: str) -> list[str] | None:
    """The VALUES tuple for one plan, split into fields.

    Written as a parser rather than a regex counting commas: the first version of this check read
    the wrong columns and reported that Business allowed one person. A check that is confidently
    wrong is worse than no check -- somebody fixes the documentation to match it.
    """
    m = re.search(rf"\('{key}',", sql)
    if not m:
        return None
    depth, out, field, in_str = 0, [], "", False
    for ch in sql[m.start():]:
        if ch == "'":
            in_str = not in_str
        if not in_str:
            if ch == "(":
                depth += 1
                if depth == 1:
                    continue
            elif ch == ")":
                depth -= 1
                if depth == 0:
                    out.append(field.strip())
                    return out
            elif ch == "," and depth == 1:
                out.append(field.strip())
                field = ""
                continue
        field += ch
    return None


def main() -> int:
    problems: list[str] = []

    for name, rel, pattern, label in PINS:
        stated, actual = fact(name), code(rel, pattern)
        if actual is None:
            problems.append(f"{name}: cannot read {label} from {rel} -- this check is now blind")
        elif stated != actual:
            problems.append(f"{name}: the docs say {stated}, {label} says {actual}")

    tiers = read(TIERS)
    for key, seats, storage, participants, send in PLANS:
        row = plan_row(tiers, key)
        if row is None:
            problems.append(f"plan {key}: not found in {TIERS} -- this check is now blind")
            continue
        # Column order is declared in the INSERT: key, name, description, price, min_users,
        # max_users, max_storage_gb, meet_max_participants, ...
        real_seats, real_storage, real_participants = row[5], row[6], row[7]
        if fact(seats) != real_seats:
            problems.append(f"{seats}: the docs say {fact(seats)}, the catalogue says {real_seats}")
        if fact(participants) != real_participants:
            problems.append(f"{participants}: the docs say {fact(participants)}, the catalogue says {real_participants}")
        # Storage is quoted with a unit for Pro ("1 TB"), so compare only where it is a plain number.
        if fact(storage).isdigit() and fact(storage) != real_storage:
            problems.append(f"{storage}: the docs say {fact(storage)} GB, the catalogue says {real_storage}")

    # The worst case we quote for backups is the sum of the two windows. Nobody computes it by hand.
    r, b, w = fact("recoveryDays"), fact("backupDays"), fact("worstCaseDays")
    if r and b and w and int(w) != int(r) + int(b):
        problems.append(f"worstCaseDays: quoted as {w}, but {r} + {b} = {int(r) + int(b)}")

    # The number of apps, as the product enforces it. "today" is the home screen, not an app.
    m = re.search(r"var toggleableApps = \[\]string\{([^}]*)\}", read("backend/internal/httpapi/apps_handlers.go"))
    if m:
        n = len([x for x in re.findall(r'"([a-z-]+)"', m.group(1)) if x != "today"])
        words = {9: "nine", 10: "ten", 11: "eleven", 12: "twelve"}
        if fact("apps") != words.get(n, str(n)):
            problems.append(f"apps: the docs say '{fact('apps')}', the product has {n}")

    # Placeholders that no fact answers render on screen as "{whatever}", literally. The check reads
    # only string content: a header comment explaining the convention mentions {key} as an example,
    # and flagging that was the first version of this check crying wolf about itself.
    known = set(re.findall(r'(\w+): "', FACTS.read_text(encoding="utf-8")))
    for f in sorted((ROOT / "landing" / "lib" / "locales" / "docs").glob("*.ts")):
        prose_only = "\n".join(l for l in f.read_text(encoding="utf-8").splitlines()
                               if not l.lstrip().startswith("//"))
        for used in sorted(set(re.findall(r"\{(\w+)\}", prose_only)) - known):
            problems.append(f"{f.name}: uses {{{used}}}, which lib/docs-facts.ts does not define")

    # Is each translation actually in its own language?
    #
    # WHY THIS EXISTS: a Dutch translation of the landing once came out in German, and nothing caught
    # it but a human reading the page. A file of a few thousand words in the wrong language looks
    # entirely correct to a compiler, to a placeholder check, and to anybody who does not speak it.
    #
    # A cheap heuristic on function words, not a language model: if another language's markers beat
    # the file's own, something is wrong enough to look at by hand.
    MARKERS = {
        "en": [" the ", " and ", " which ", " your "],  # lang:ok — these are language samples, not prose
        "pt": [" que ", " não ", " uma ", " para "],  # lang:ok — these are language samples, not prose
        "es": [" que ", " los ", " una ", " para "],  # lang:ok — these are language samples, not prose
        "fr": [" les ", " que ", " vous ", " pour "],  # lang:ok — these are language samples, not prose
        "de": [" der ", " und ", " nicht ", " Sie "],  # lang:ok — these are language samples, not prose
        "it": [" che ", " gli ", " una ", " per "],  # lang:ok — these are language samples, not prose
        "nl": [" het ", " een ", " niet ", " voor "],  # lang:ok — these are language samples, not prose
        "pl": [" nie ", " jest ", " oraz ", " Państwa "],  # lang:ok — these are language samples, not prose
    }
    for f in sorted((ROOT / "landing" / "lib" / "locales" / "docs").glob("*.ts")):
        lang = f.stem
        if lang not in MARKERS:
            continue
        body = "\n".join(l for l in f.read_text(encoding="utf-8").splitlines()
                         if not l.lstrip().startswith("//"))
        score = {k: sum(body.count(m) for m in ms) for k, ms in MARKERS.items()}
        best = max(score, key=lambda k: score[k])
        # Spanish and Portuguese share markers; only complain when a clearly different family wins.
        family = {"pt": {"es"}, "es": {"pt"}}
        if best != lang and best not in family.get(lang, set()):
            problems.append(
                f"{f.name}: reads more like {best} than {lang} "
                f"({score[best]} vs {score[lang]} marker hits) — check it was not translated into the wrong language")

    if problems:
        print("DOCUMENTATION OUT OF DATE -- the code and lib/docs-facts.ts disagree:\n")
        for p in problems:
            print(f"  {p}")
        print("\nFix the fact, or the constant. Not neither.")
        return 1
    print("docs: every pinned number matches the code")
    return 0


if __name__ == "__main__":
    sys.exit(main())
