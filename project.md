Project brief — Email + storage platform (Google Workspace alternative)


This document describes the product, the non-negotiable constraints, the architecture, and the phase plan.
Read the "How you should work" section before writing any code.




1. What we are building

A multi-tenant email and storage platform to sell as a product (SaaS), self-hosted on our own VPS. Each customer (tenant) is an organization with its own domain, its own users, and its own mailboxes.

The email engine is Stalwart — an all-in-one server (SMTP/IMAP/JMAP/DKIM/spam/storage) that we use without modification, only through its API. On top of it we build a control plane (Go backend) and our own interface (React frontend). All the differentiation, the user experience, and the business logic are ours.

Scope of the first version: email (send/receive/read) + file storage + the multi-tenant management panel that provisions and bills customers. Calendar, contacts, and collaboration come later.


2. Non-negotiable constraints (violate none)


NEVER modify Stalwart. Do not fork it, do not patch its source code, do not recompile it with changes, do not link/embed its code into our binary. We run the official binary/image and only talk to it over the network (JMAP + REST Management API). Editing configuration files and running it via Docker is allowed; changing the source code is not. This is a license requirement (AGPL-3.0): treating Stalwart as an external service keeps all of our code proprietary and closed.
Multi-tenancy is ours, in Go. Formal tenancy with per-customer admin and branding is a paid Stalwart Enterprise feature. We do NOT use it. We implement the tenant concept, per-customer admin, and isolation between organizations in our own Go layer, using Stalwart Community's multi-domain support (each customer = one or more domains).
Stalwart's Admin API is GLOBAL admin and must never leak to the customer. Every administrative operation (create domain, mailbox, change quota) mandatorily goes through our Go backend, which first checks whether the authenticated customer owns the resource and only then translates it into the API call. No Stalwart admin credential reaches the frontend, not even through a proxy.
End-user isolation is native to Stalwart — do not reimplement it. Each mailbox is a principal with its own authentication; JMAP only ever hands back what the credential can see. Do not build an authorization layer on top of that for reading email.
Pin the Stalwart version and generate the API client from that version's OpenAPI spec. Stalwart is pre-1.0; endpoints and payloads change between versions. Do not hardcode payloads from memory — generate types from the openapi.yml of the exact version we run.
Deliverability is a separate problem from code and is the biggest business risk. rDNS/PTR, IP warm-up, SPF/DKIM/DMARC, feedback loops, and blocklist monitoring are not solved with pretty code. The system must have hooks for this (per-tenant sending limits, outbound IP pools), but do not assume that provisioning well equals delivering well.



3. Technical stack


Frontend: React (TypeScript). Webmail + customer panel + internal admin panel + billing and storage UI.
Backend (control plane): Go. REST/JSON API, workers, integration with Stalwart and Stripe.
Database: PostgreSQL (our control plane and Stalwart's data backend can share the same Postgres, but in separate schemas/instances).
Blob storage: MinIO (S3-compatible, self-hosted) for our file service.
Email engine: Stalwart (official, unmodified), via Docker.
Payments: Stripe (or equivalent) for subscriptions tiered by mailbox count.
Infrastructure: Docker Compose to start; leave a path open to multiple Stalwart instances later.



4. Architecture

React (TypeScript)
 ├─ Webmail            → talks JMAP DIRECTLY to Stalwart (end user's token)
 ├─ Customer panel     → talks to our Go API
 ├─ Admin panel        → talks to our Go API
 └─ Storage UI         → talks to our Go API

Go (control plane — what we build and sell)
 ├─ Auth / accounts / tenants / orgs
 ├─ Provisioning: creates domains, DKIM, mailboxes by calling Stalwart's Admin API
 ├─ Billing (Stripe): subscriptions, usage-based invoicing
 ├─ Storage service: upload/download/share on top of MinIO
 ├─ Per-tenant sending quotas + abuse detection
 └─ Tenant → Stalwart instance routing (to scale later)

Stalwart (engine, NOT modified)  → SMTP/IMAP/JMAP/DKIM/spam
MinIO                               → file blobs
PostgreSQL                          → control plane + Stalwart backend

Core principle: the end user reads email by talking JMAP directly to Stalwart; our Go code only steps in for administrative, billing, and storage operations. This keeps Go out of the critical read path (less latency, less attack surface).


5. Data model (Postgres — control plane)

Starting point; adjust as needed. Every query on customer data MUST filter by tenant_id (the standard multi-tenant pattern in a shared database).

tenants        (id, name, plan, status, stalwart_instance, outbound_ip_pool, created_at)
domains        (id, tenant_id, domain, stalwart_id, dkim_status, dns_status, provision_state)
mail_users     (id, tenant_id, email, name, role, stalwart_account_id, quota_bytes)
storage_files  (id, tenant_id, owner_user_id, name, blob_key, hash, size, version, created_at)
storage_shares (id, file_id, tenant_id, principal, level)   -- owner/editor/reader/link
usage          (tenant_id, storage_bytes, sent_today, period)
subscriptions  (tenant_id, stripe_id, mailbox_tier, status)
audit_log      (id, tenant_id, actor, action, target, timestamp)

stalwart_id / stalwart_account_id are the link between our world and Stalwart's.


6. Integration with Stalwart (real endpoints)

Authentication: our Go code uses a global admin credential (Bearer token or basic auth). In Stalwart, domain, user, and group are all "principals", distinguished by the type field. Most management is via JMAP (POST /jmap/ with the urn:stalwart:jmap capability); the REST endpoints below are convenience helpers for provisioning.


Create domain: POST /api/principal with {"type":"domain","name":"customer.com.br","description":"tenant_id=42","quota":0} → store the returned id. (Requires the sysDomainCreate permission on the admin token.)
Generate DKIM: POST /api/dkim with domain, algorithm (e.g. ed25519), selector. Confirm the exact fields in the OpenAPI spec of the version we run.
Create mailbox: POST /api/principal with {"type":"individual","name":"joao","secrets":["password"],"emails":["joao@customer.com.br"],"quota":5368709120,"roles":["user"]} → store the returned id. Careful: name is the login and is unique across the WHOLE server, not per domain — qualify it when there is a collision. emails are the actual addresses.
Read/edit/remove principal: GET|PATCH|DELETE /api/principal/{id} (PATCH uses set/addItem/removeItem actions).
Login / token (webmail): POST /api/auth authenticates and returns an authorization code; exchange it for a token (OAuth 2.0 / OIDC supported).
Pre-created OIDC accounts: POST /api/principal/deploy (same payload as principal) when we use an external OIDC directory.
Queue / diagnostics: GET /api/queue/messages and the troubleshoot tools (simulate delivery, check SPF/DKIM/DMARC).


Rule: generate a typed Go client from the openapi.yml of the pinned version. Do not write payloads by hand.


7. Provisioning state machine (implement it this way)

Provisioning a new customer involves calls that can fail partway through and a step (DNS) that depends on the customer and takes time. Model it as a persisted state machine, not a linear script. Every transition is idempotent and recoverable by a worker.

pending → domain_created → dkim_created → dns_pending → dns_verified → active
                                                     ↘ (failure) → error


pending: signup creates the tenant + Stripe customer; enqueues provisioning; responds to React immediately.
domain_created: POST /api/principal (domain); saves stalwart_id.
dkim_created: POST /api/dkim.
dns_pending: returns the DNS records to the customer (MX, SPF, DKIM TXT, DMARC, optional TLSA/MTA-STS); waits until the customer publishes them.
dns_verified: worker polls (its own DNS resolution and/or troubleshoot) until the records match.
active: creates the mailboxes (individual), saves stalwart_account_id; customer ready.


The worker always resumes from the last saved state, never from scratch.


8. Authentication and webmail handoff


Administrative and billing operations: React → our Go API (Go holds the Stalwart admin credential, never the customer).
Reading/writing email: React → JMAP directly to Stalwart with the end user's token. Flow: React sends credentials to POST /api/auth → receives a code → exchanges it for a token → uses it as Bearer in POST /jmap/.
Unified login (optional, later phase): run our own OIDC provider and configure Stalwart with an external OIDC directory; pre-create accounts via POST /api/principal/deploy.



9. Delivery phases (work in this order, one at a time)

Phase 0 — Foundation. Docker Compose with official Stalwart + Postgres + MinIO running locally. Typed Go client generated from the pinned version's OpenAPI spec. Go project skeleton (API + workers) and React skeleton. Basic authentication for our panel.

Phase 1 — Provisioning. The full state machine from section 7: signup, domain creation, DKIM, DNS screen, verification, mailbox creation. Idempotent and tested. Deliverable: I can create a new customer end to end through the panel.

Phase 2 — Webmail. React consuming JMAP directly (list/read/send/search email, folders, attachments). Auth handoff from section 8.

Phase 3 — Storage. Go service on top of MinIO: multipart/resumable upload, hash-based dedup, versioning, sharing (owner/editor/reader/link). React UI.

Phase 4 — Billing and quotas. Stripe (subscriptions tiered by mailbox count), disk and sending quotas per tenant, usage-based invoicing, usage panel.

Phase 5 — Operations and compliance. Sending limits + per-tenant abuse detection, reputation/IP-pool hooks, abuse@, audit_log, LGPD (privacy policy, DPA, data export/deletion), tenant → instance routing to scale.

Do not skip phases. Do not start Phase N+1 before Phase N is tested and working.


10. Non-functional requirements


Idempotency in every provisioning step and webhook (Stripe, retries).
Tests at every phase (unit + integration against a test Stalwart in a container).
Security: Stalwart credentials only in the backend; secrets outside the code; tenant-ownership check before every admin operation; rate limiting.
LGPD (Brazilian product): we are data processors; we need a legal basis, a DPA with customers, handling of abuse@, and data export/deletion per tenant.
Observability: structured logs, queue and delivery metrics, blocklist alerts.
Shared reputation: one tenant sending spam brings down everyone on the same IP — sending limits and abuse detection are a requirement, not a nice-to-have.



11. How you should work (AI, read this)


Start with Phase 0 and go one phase at a time. When each phase is done, stop, show what was done and how to test it, and wait for confirmation before moving on.
Before structural decisions (library choices, auth format, final schema), propose the options with trade-offs and ask — do not decide alone and proceed.
Never modify Stalwart. If something seems to require changing Stalwart, stop and report it — there is probably a path through the API, or it is a sign that we need the Enterprise edition.
Pin the Stalwart version early on and generate the client from its OpenAPI spec. Do not invent payloads.
Write tests alongside each phase's code, not afterward.
Provisioning and webhooks are idempotent by default. Assume any network call can fail and be retried.
Document decisions and setup in a README as you go.
If anything in this brief conflicts with the reality of the Stalwart version you find, trust the real version and let me know about the divergence.



First step

Confirm you understand the scope and the constraints, propose the concrete choices for Phase 0 (Stalwart version to pin, Go and React libraries, repository layout) with trade-offs, and wait for my confirmation before generating code.
