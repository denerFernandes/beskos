#!/usr/bin/env python3
"""Law 4.8: every user-facing string is formal and corporate.

Each language has one courtesy form and we use it. This checks the opposite: the informal address
that slips in when a sentence is written the way it would be spoken. Those markers are unambiguous
per language, which is why this can be a grep and not a judgement call.

The patterns are deliberately narrow. An earlier, wider set flagged Spanish "se crea" and "la
prueba" (a noun) and Italian "sei" (also the number six), and a checker that cries wolf gets
switched off. Verb forms shared with the third person are NOT listed here: only pronouns,
possessives and verb forms that exist solely in the informal second person.
"""

import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent

# Where user-facing text lives. Anything here is read by a customer.
SURFACES = [
    ("landing/lib/locales", "*.ts"),
    ("frontend/src/i18n/locales", "*.json"),
]

# Informal address, per language. Matched case-insensitively except where noted.
INFORMAL = {
    "pt": [r"\btu\b", r"\bteu\b", r"\bteus\b", r"\btua\b", r"\btuas\b", r"\bcontigo\b"],
    "es": [r"\btú\b", r"\btu\b", r"\btus\b", r"\bti\b", r"\bcontigo\b",  # lang:ok
           r"\bpuedes\b", r"\btienes\b", r"\bquieres\b", r"\bdebes\b", r"\bhaces\b"],
    "fr": [r"\btu\b", r"\bton\b", r"\btes\b", r"\btoi\b", r"\btoi-même\b", r"\bpeux\b", r"\bveux\b"],  # lang:ok
    "de": [r"\bdu\b", r"\bdein\w*\b", r"\bdich\b", r"\bdir\b", r"\bkannst\b", r"\bhast\b"],
    "it": [r"\btuo\b", r"\btua\b", r"\btuoi\b", r"\btue\b", r"\bpuoi\b", r"\bdevi\b", r"\bhai\b",
           r"\bvostro\b", r"\bvostra\b", r"\bvostri\b", r"\bvostre\b"],
    "nl": [r"\bje\b", r"\bjij\b", r"\bjouw\b", r"\bjou\b", r"\bjullie\b"],
    "pl": [r"\btwój\b", r"\btwoja\b", r"\btwoje\b", r"\btwoją\b", r"\btwojego\b", r"\btwojej\b",  # lang:ok
           r"\btwoim\b", r"\btwoich\b", r"\btwoja\b", r"\bmożesz\b", r"\bmasz\b", r"\bjesteś\b"],
    "en": [],  # English has no formal/informal distinction. The shared rules below still apply.
}

# The Italian "voi" form is not informal, but it is not "lei" either. One register per language.
NOT_LEI = {"vostro", "vostra", "vostri", "vostre"}

EMOJI = re.compile(
    "[\U0001F300-\U0001FAFF\U00002600-\U000027BF\U0001F1E6-\U0001F1FF⬀-⯿]"
)

# A comment is not user-facing. Neither is an import or a bare identifier.
COMMENT = re.compile(r"^\s*(//|/\*|\*)")


def strings_in(line: str) -> str:
    """The quoted text on a line, which is the only part a customer ever reads."""
    return " ".join(re.findall(r'"((?:[^"\\]|\\.)*)"', line))


def language_of(path: Path) -> str:
    """The locale a file belongs to: locales/<lang>/x.ts or locales/docs/<lang>.ts.

    A regional tag maps to its base language: "pt-BR" is checked with the Portuguese markers.
    """
    name = path.stem if path.parent.name == "docs" else path.parent.name
    return name.split("-")[0]


def check() -> list[str]:
    problems: list[str] = []
    for base, glob in SURFACES:
        for path in sorted((ROOT / base).rglob(glob)):
            # An unknown language still gets the rules that do not depend on one. Skipping the whole
            # file was how the pt-BR dictionary kept its emoji and its exclamation marks: the
            # informal markers did not apply to it, so nothing applied to it.
            lang = language_of(path)
            rel = path.relative_to(ROOT)
            for n, line in enumerate(path.read_text(encoding="utf-8").split("\n"), 1):
                if COMMENT.match(line):
                    continue
                text = strings_in(line)
                if not text:
                    continue
                for pattern in INFORMAL.get(lang, []):
                    hit = re.search(pattern, text, re.IGNORECASE)
                    if not hit:
                        continue
                    word = hit.group(0)
                    kind = "not the 'lei' form" if word.lower() in NOT_LEI else "informal address"
                    problems.append(f"{rel}:{n}: {kind} -- '{word}' (law 4.8)")
                    break
                if EMOJI.search(text):
                    problems.append(f"{rel}:{n}: emoji in user-facing text (law 4.8)")
                if re.search(r"!(?![=\]])", text):
                    problems.append(f"{rel}:{n}: exclamation mark (law 4.8)")
    return problems


if __name__ == "__main__":
    found = check()
    for p in found:
        print(f"  {p}")
    if found:
        print(f"\nregister: {len(found)} lines break law 4.8 (formal, corporate text)")
        sys.exit(1)
    print("register: every user-facing string uses the formal address")
