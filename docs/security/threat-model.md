# Threat model

A first pass, scoped to the boundaries that matter most for a multi-tenant product built around
a mail engine it does not control the source of. Not exhaustive; extend it when a new trust
boundary is added, and revisit it before F9 (docs/OPEN-SOURCE.md).

## Trust boundaries

```
                        ┌─────────────────────────────────────────────┐
                        │                 Internet                     │
                        └───────────────────────┬───────────────────────┘
                                                 │ HTTPS only
                    ┌────────────────────────────┴────────────────────────────┐
                    │           reverse proxy (same-origin, required)          │
                    └───┬─────────────┬─────────────┬─────────────┬───────────┘
                        │             │             │             │
                  ┌─────▼─────┐ ┌─────▼─────┐ ┌─────▼──────┐ ┌────▼─────┐
                  │  Go API   │ │  Stalwart │ │ OnlyOffice │ │ LiveKit  │
                  │(this repo)│ │  (JMAP)   │ │  Document  │ │ (SFU +   │
                  │           │ │           │ │  Server    │ │ Egress)  │
                  └─────┬─────┘ └─────┬─────┘ └─────┬──────┘ └────┬─────┘
                        │             │             │             │
                 ┌──────▼──────┐      │       ┌─────▼──────┐      │
                 │  Postgres   │      │       │ blobgw (S3 │◄─────┘
                 │ (shared,    │      │       │  signing   │
                 │ tenant_id)  │      │       │  proxy)    │
                 └─────────────┘      │       └─────┬──────┘
                                       │             │
                                 ┌─────▼─────┐ ┌─────▼──────┐
                                 │ mail data │ │   object   │
                                 │(Stalwart) │ │  storage   │
                                 └───────────┘ └────────────┘
```

## 1. Tenant isolation (Postgres, shared schema)

**Boundary:** every organization's data lives in the same tables, distinguished only by
`tenant_id`. **Attacker:** an authenticated user of tenant A, trying to read or write tenant B's
data. **Control:** every handler resolves `tenant_id` from the authenticated session, never from
a request parameter, and every sqlc query touching customer data filters by it (law 5.1).
**Verification:** `scripts/check_tenant_scoping.py` (report-only, heuristic -- flags a query
against a tenant-scoped table that never mentions `tenant_id`, including via ownership columns
like `owner_user_id` it can't recognize as equivalent; every hit needs a human to actually read
the query). **Residual risk:** a new query that forgets the filter ships silently until the
checker or a manual review catches it -- there is no runtime enforcement (e.g. Postgres row-level
security) backing this up. Worth considering once the schema stabilizes.

## 2. JMAP credential brokering (webmail ↔ Stalwart)

**Boundary:** the browser never holds a Stalwart account credential. **Attacker:** a user trying
to read another user's mailbox, or exfiltrate the shared Stalwart admin credential.
**Control:** the webmail authenticates to *this* API with a session cookie; the API looks up the
user's own encrypted JMAP credential (`internal/secretbox`, AES-GCM) and injects it server-to-
server when proxying to Stalwart. The admin credential (`STALWART_ADMIN_SECRET`) is read only by
the Go processes and is never returned in any API response. **Verification:** `grep -rn
StalwartAdminPwd backend/internal/httpapi` should only ever show it flowing into the
`stalwart.Client` constructor, never into a response body -- worth scripting the same way as the
tenant-scoping check if this file grows large enough to drift. **Residual risk:** the encrypted
credential's key (`MFA_ENC_KEY`) is a single shared secret; its rotation story
(docs/OPEN-SOURCE.md notes this as an open architectural item) is not yet designed.

## 3. blobgw (S3-compatible signing proxy)

**Boundary:** blobgw sits between OnlyOffice/LiveKit Egress and object storage, and is reachable
only on the internal network -- but "internal network" is an infrastructure assumption, not a
property of the service, so it verifies AWS SigV4 signatures anyway (`cmd/blobgw/sigv4.go`):
access key, a 15-minute clock-skew window against replay, and an HMAC over the canonical request.
**Attacker:** anything that reaches blobgw's port without holding a valid signing key -- an
internal network misconfiguration, a container that shouldn't have this route, a future service
added carelessly to the same network. **Residual risk:** correct by inspection of the signature
verification logic; not covered by an automated test that forges a bad signature and asserts a
rejection -- worth adding (see F5's fuzzing item in docs/OPEN-SOURCE.md).

## 4. OnlyOffice Document Server callback

**Boundary:** the Document Server calls back into the API (save the edited document, fetch it for
editing) over a JWT shared secret (`ONLYOFFICE_JWT_SECRET`), separate from the token that signs
the download/callback URLs themselves (`DOC_TOKEN_SECRET`). **Attacker:** something other than
our own configured Document Server instance, claiming to be delivering a saved document.
**Control:** the JWT is verified before any callback is trusted; production refuses to boot
without both secrets set to a real (non-default, ≥16-char) value (`config.validateProduction`).
**Residual risk:** not yet verified that the callback handler checks the JWT's claims (which
document, which action) match the request, rather than only checking the signature is valid for
*some* JWT issued with the shared secret -- worth a close read before F9.

## 5. Webhooks (Stripe, SES/SNS)

**Boundary:** external services push events into the API without an interactive session.
**Control:** Stripe's webhook signature is verified (`internal/billing/stripe.go`) and events are
deduplicated by `event_id` (idempotency, law 5.3); SES/SNS events go through
`internal/ses.SNSVerifier`. **Residual risk:** neither path has an automated test that sends a
tampered signature and asserts rejection -- same gap as blobgw, same fix.

## 6. Encrypted file storage

**Boundary:** uploaded files are encrypted client-side (Go process, before the object leaves for
S3/MinIO) with AES-256-GCM in blocks; the object's key travels inside the object itself.
**Attacker:** anyone with read access to the object storage bucket directly (a misconfigured
bucket policy, a compromised storage credential) but not to the application or its database.
**Residual risk:** the encryption key material's own rotation and the blast radius of a leaked
`FILE_ENC_KEYS` value are not documented anywhere yet -- see
[docs/adr/0004-aes-gcm-block-file-encryption.md](../adr/0004-aes-gcm-block-file-encryption.md)
for the scheme and close this gap there, not here.

## 7. Rate limiting and abuse

Present today: MFA verification capped at 5 attempts per challenge. **Not yet present:** a global
per-IP or per-account limit on login, signup, or password recovery -- see the roadmap in the
project README. An attacker with no rate limit on `/auth/login` can brute-force a weak password at whatever rate
the server can answer; this is the single highest-priority item left on this list.

## Out of scope for this document

Stalwart's and OnlyOffice's and LiveKit's own internal security are their upstream projects'
responsibility -- this document covers how *this* codebase talks to them, not their internals.
A vulnerability in how this project configures or calls one of them is in scope; report it per
[SECURITY.md](../../SECURITY.md).

## Open items (tracked, not yet done)

- Automated tests that forge a bad signature/JWT/webhook and assert rejection, for blobgw,
  OnlyOffice callbacks, and the Stripe/SNS webhooks.
- Global rate limiting on login, signup, and password recovery.
- A designed rotation story for `MFA_ENC_KEY` and `FILE_ENC_KEYS`.
- Row-level security in Postgres as a second line of defense behind the query-level `tenant_id`
  filter, if the manual/heuristic checking in `scripts/check_tenant_scoping.py` proves
  insufficient at scale.
