# Domain DNS page — planned improvement

**Status: implemented on 2026-07-27.** Per-record verification (`provisioning.CheckZone`),
endpoints `GET /api/domains/{id}/dns/check` and `.../export`, its own **page**
(`/mail/admin/domains/<id>`, used to be a sidecar), per-record status with expected vs. published,
a summary, "check now", BIND/CSV zone export, and a per-provider guide (Cloudflare, Route 53,
GoDaddy, Namecheap, Google, other).

## What exists today

- **Backend** — [`domains_handlers.go`](../backend/internal/httpapi/domains_handlers.go) returns
  `dns_records` (Stalwart's zonefile parsed by
  [`provisioning.ParseZone`](../backend/internal/provisioning/zone.go)) and two text states:
  `dns_status` and `dkim_status`.
- **Verification** — [`NetDNSVerifier.Verify`](../backend/internal/provisioning/dns.go) returns
  **a single boolean** for the whole domain: it looks for the MX and **one** `_domainkey` record
  with `DKIM1`. It doesn't look at SPF, DMARC, MTA-STS, autodiscover, or the SRV records. The
  worker polls this until it passes (`stepVerifyDNS`), and the domain jumps from `dns_pending` to
  `dns_verified`.
- **Frontend** — [`DomainsSection.tsx`](../frontend/src/panel/DomainsSection.tsx) shows a single
  table (Purpose · Type · Host · Value · Copy), split into "receive" and "send".

## Why it's confusing

1. **Status only at the domain level, not the record level.** It says "waiting" without saying
   *for what*. Someone who has published 9 of 11 records has no way to know which ones are
   missing — they don't even know that verification only looks at 2 of them.
2. **Verification lies by omission.** It passes with MX + DKIM present; a missing SPF or DMARC
   still reads as "verified," and the problem only surfaces once mail starts landing in spam.
3. **Copying one at a time.** There are ~11 records; copying field by field into the provider's
   panel is slow and error-prone (quotes, trailing dot, MX priority).
4. **The table overflows** on narrow screens: the long values (DKIM, MTA-STS) break the layout —
   which is what the current screen shows, with the "Copy" column broken into vertical letters.

## What to do

### 1. Per-record verification (backend)

Replace the boolean with a report. Sketch:

```go
type RecordCheck struct {
    Host     string   // as it is in the zone
    Type     string   // MX, TXT, CNAME, SRV
    Expected string
    Found    []string // what the resolver returned (to show what's actually there, not just the expected value)
    Status   string   // "ok" | "missing" | "mismatch"
    Purpose  string   // mx, spf, dkim, dmarc, mta_sts, autodiscover, srv…
}
```

- `Verify` now returns `[]RecordCheck`, and the aggregate boolean is derived from the ones that
  are **required** (MX, SPF, DKIM) — the recommended ones (DMARC, MTA-STS, SRV) become a warning,
  not a blocker. Today it's the opposite: they're ignored entirely.
- New endpoint `GET /api/domains/{id}/dns/check` that runs the verification **on demand** (the
  worker's polling stays as it is, so as not to change the state machine). Its own rate limit:
  it's network I/O.
- Short cache (30–60s) per domain, otherwise a repeated refresh hammers the resolver.

### 2. Page with granular status (frontend)

- One row per record with a clear signal: **found** / **missing** / **different value**.
- When it's "different value," show the expected and the published value side by side — it's the
  hardest case to diagnose blind (typical cases: a duplicate SPF, or the old provider's MX).
- Group by purpose (Receive mail · Send · Client discovery · Security), with the group's status in
  its header and the ability to collapse groups already resolved.
- **"Check now"** button wired to the endpoint above, with the timestamp of the last check.
- Fix the overflow: the value in a `code` block with its own `overflow-x:auto`, never pushing the
  table itself.

### 3. Export

- **BIND zone file** (`.zone`) — the format Cloudflare, Route 53 and most providers import. It's
  also what Stalwart already produces, so it's nearly just serving what already exists:
  `GET /api/domains/{id}/dns/export?format=zone`.
- **CSV** for anyone editing by hand.
- Copy everything at once to the clipboard, in addition to the existing per-record copy.

> Note: the "Cloudflare format" **is** the BIND zone file — their panel imports exactly that
> (`DNS › Records › Import`). No proprietary format needed.

### 4. Documentation — DONE (per-provider guide on the page itself)

Link to a per-provider guide (Cloudflare, Route 53, GoDaddy, Namecheap) from the page. The guide
doesn't exist yet; create it under `docs/` or on the public site and link to it from here.

## What's left undone

- Grouping by purpose with already-resolved groups collapsed: today the two tables
  (receive/send) remain, which was already the existing grouping.

## Caveats

- **Multi-tenancy (law §5.1):** the domain has to belong to the authenticated user's tenant — the
  on-demand verification can't turn into a DNS scanner for other people's domains.
- **Don't touch the state machine** when adding on-demand verification: `stepVerifyDNS` still
  drives the `dns_pending → dns_verified` transition. The page informs; it doesn't promote.
- **DNS lies through caching.** Always show the timestamp of the lookup and avoid promising
  immediate propagation; a record published just now may not show up for several minutes.
