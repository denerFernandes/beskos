# Plan — envelope encryption of files, with our own key

> Status: **live** since 2026-07-30. Written as a plan on 07/29.
> Envelope encryption in our own backend (not SSE-C, not E2EE). Recordings and backups
> included. Read §2f before touching the sealing logic — it cost 43 emails.

## 1. The problem, stated precisely

Files **are** encrypted at rest — by Cloudflare R2, with AES-256, transparently. The security page
says exactly that ("encrypted at rest **by the provider**"), so there's no false claim published.
What's missing is encryption with **our own key**.

In [`storage.go`](../backend/internal/storage/storage.go), `PutObject` goes out with no encryption
option at all: we write bytes in the clear and the provider encrypts them with its own key.

Three consequences, in order of severity for our positioning:

1. **Cloudflare holds the key.** It is the only US company in the stack that stores customer
   *content* — the other two handle transit and payment metadata. It's the heaviest line in the
   `/sovereignty` table, and the first thing a data protection officer points to.
2. **A leaked R2 credential exposes everything.** Today it grants access to the content; afterward
   it would only grant access to ciphertext.
3. **We ourselves can read the files.** Legitimate, and the same as Google and Microsoft — but not
   what we claim to be selling.

**Why not E2EE.** End-to-end encryption would break what the product promises: a public link
without an account, a large attachment sent by email outside the organization, browser-based
Office editing, thumbnails, video `Range` requests, and search. Proton can do it because it has
none of that. It stays a future option, opt-in per folder, with the limitations stated up front.

## 2. The design

**Envelope encryption, inside the storage service.** R2 now stores only ciphertext; the keys stay
in our own infrastructure, in the EU. Shares keep working because the server decrypts when
serving.

### 2a. Where the code plugs in

All file traffic already goes through our backend — **there are no pre-signed URLs anywhere**
(verified: zero occurrences of `Presign` in the repository). That's what makes this possible
without touching the client protocol or the desktop app.

There are **33 call sites** (15 `Get`, 8 `Put`, 5 `Exists`, 3 `Remove`, 2 `GetSeek`), spread across
files, versions, chat, wiki, boards, presentations, recordings and OnlyOffice. **None of them
change:** the encryption goes inside `storage.Service`, the only thing that talks to the bucket.

### 2b. Key hierarchy

```
KEK (environment, versioned)  →  tenant key  →  DEK (one per blob)  →  object
```

- **DEK per blob**, random, 256 bits. Deduplication still works: the object key is
  `tenant/sha256(content)` and the DEK is *per object*, so the same content in the same tenant is
  still a single blob. (Convergent encryption — a key derived from the content — would allow
  dedup across tenants, but reveals whether a file already exists on the platform. Not worth it.)
- **The DEK travels INSIDE the object**, wrapped, in the header — not in a table. See §2d: this is
  what makes every write atomic. Whoever stores the object ends up with the wrapped key and can't
  open it: they would need the tenant key (in our database) and the KEK (in our environment). It's
  the same design as AWS's and Tink's client-side encryption, for the same reason.
- Only the **tenant key** lives in a table (`tenant_keys`), wrapped by the KEK.
- **Per-tenant key** for the same reason every query filters by `tenant_id`: deleting a tenant's
  key makes their data unreadable, which is a much stronger deletion guarantee than "we deleted
  the rows" — and it's auditable in the data processing agreement.
- **Versioned KEK** in the environment, the same way `MFA_ENC_KEY` already works for TOTP.
  Rotating the KEK re-wraps keys (fast, small table) and **doesn't rewrite a single object** —
  that's the whole point of the envelope.

### 2c. Object format

Fixed-size blocks, so `Range` keeps working. Without this, video seeking and the sync client's
block-by-block reads would break.

```
header (32) | wrapped DEK | block 0 | block 1 | ... | block n
```

- **Header:** magic `BSK1`, version, options, block size, plaintext size, nonce prefix, KEK
  version, wrapped DEK length. Fixed at 32 bytes: 32 bytes is enough to know where block 0 starts
  and whether it's worth reading further.
- **Block:** AES-256-GCM, nonce = prefix ‖ block index, associated data = index + last-block flag
  (prevents reordering and truncation).
- **Block size: 1 MiB.** 16 bytes of overhead per block (0.0015%) and fine enough granularity for
  seeking. A byte-range request translates to blocks; only those get decrypted.

### 2d. One write, and why

The original plan stored the DEK in a table and wrote the object afterward, choosing the order
whose failure window was benign. **That was abandoned**: with two writes, *every* order has a
window — object-first leaves ciphertext with no key (data loss), row-first leaves orphaned rows.
"Benign" is a promise that only holds as long as nobody touches the code.

The DEK now travels **inside the object itself**, wrapped by the tenant key. That leaves **a
single write**, and the storage `PUT` either happens whole or doesn't happen at all. There's no
ordering to get right because there are no longer two things to order.

Gains that came along with it: one fewer table; `Get` no longer does a query per file read; and
`Remove` goes back to being a single row deletion, because the key dies with the object.

### 2e. Migrating objects that already exist

Detection is by the **magic bytes in the header** — from the object itself, not from a column that
could drift out of sync. That makes the migration idempotent and restart-proof: a database flag
that failed to write would end up encrypting already-encrypted data.

A worker job ([`sealing.go`](../backend/cmd/worker/sealing.go)) walks the bucket and seals in the
background, with a pause between objects — this runs on the same server that serves customer mail,
and migrating fast isn't worth interrupting people at work. A pass that doesn't finish isn't a
problem: the next one skips whatever's already done.

## 2f. The bucket isn't only ours

Discovered on the very first pass in production, the hard way: sealing encrypted **43 email
messages**, and Stalwart could no longer read them. It stores them under `mail/…`, in the same
bucket, written and read by itself, never going through this code.

The cause wasn't an unforeseen case — it was the **wrong question**. The function was asking "does
this look safe to encrypt?" when it needed to ask "**did we write this?**". It now uses an
allowlist: `<tenant>/…` and `recordings/…`, the only key families `storage.Service` writes. A
denylist would have to guess every future neighbor, and a shared bucket only ever gains neighbors.

The rollback lives in [`cmd/unseal`](../backend/cmd/unseal/main.go), and stays in the repository:
if this happens again, we want the rollback ready, not a program written in a hurry during the
outage.

**Two consequences, both recorded on the public pages:**

1. **Mail content is not encrypted by us.** It's outside the scope of this design as long as
   Stalwart writes directly to the bucket — and we don't modify Stalwart (docs/engineering-standards.md §5.4). The
   security page states this in our own words instead of leaving it to be discovered.
2. **Attachments and the original messages are NOT in the backup.** Measured on 07/30 by
   restoring the encrypted backup into a disposable Postgres and comparing it against the bucket:

   | | Postgres (table `p`) | R2 (`mail/`) |
   |---|---|---|
   | Total | 115 KiB in 110 rows | **637 KiB** in 43 files |
   | Largest item | 7,885 bytes | **552,638 bytes** |

   Stalwart splits it: Postgres holds the **parsed** message (sender, subject, search terms, body
   text) and R2 holds the **raw** message with attachments, which is what IMAP delivers when a
   client asks for the original. A 540 KiB object doesn't fit in a database whose largest row is
   7.9 KiB — and this was confirmed slice by slice: a chunk from the middle of the large message
   is in the database, the one from the end isn't.

   In other words, `pg_dump` alone **does not restore mail**. It gives back mailboxes, accounts and
   searchable content; it loses the attachments and the original. And R2 has no second copy of
   ours.

   **What's left to decide:** whether to include the bucket's `mail/` prefix in the backups. It's
   cheap — 637 KiB today — and it's the difference between "we have copies" and "we can restore."
   Recorded here rather than on the security page, which in the meantime stopped claiming what
   wasn't actually known.

## 3. The two paths that bypass the backend

This was the most significant finding of the review, which is why it's part of this phase.

### 3a. Meeting recordings

**LiveKit Egress writes directly to the bucket** with S3 credentials
([`meet_recording_tracks.go`](../backend/internal/httpapi/meet_recording_tracks.go)). Any
encryption done in the backend would never touch these.

- **Per-track** recordings are already downloaded by the worker, assembled with ffmpeg and
  **re-uploaded** via `storage.Put` ([`assembly.go`](../backend/cmd/worker/assembly.go)) — those
  get encrypted for free, and the raw tracks are deleted afterward.
- **Composite** recordings (RoomComposite) are written by egress and stay that way. They're sealed
  by the **same pass** that migrates old content (§2e) — it's the same problem (an object that
  didn't go through our encryption) and didn't deserve two mechanisms. Sealing rewrites the same
  key: one write, atomic; whoever is reading at that instant gets either the old version or the
  new one, and both are readable.
- **New** recording keys carry a **tenant prefix** (`<tenant>/recordings/…`). Without it, the
  recording would be wrapped by the KEK and left out of per-tenant deletion — deleting a tenant's
  key has to make everything of theirs unreadable, recordings included. Old ones, without the
  prefix, are sealed with the KEK.
- **There's a plaintext window** left between the end of egress and sealing. It's honest to say
  so. Closing it would require egress to write to a destination of ours, which LiveKit doesn't do
  without an intermediate S3 server.

### 3b. Backups

[`backup.sh`](../deploy/backup/backup.sh) sends the dumps via `rclone` **unencrypted**, and the
[README](../deploy/backup/README.md) itself already flags the gap: *"whoever holds the R2 key can
read the dumps."* This includes the mail database — messages, attachments and mailbox
configuration.

- Encrypt with **`age`**, with an **asymmetric** key: the server only carries the public key. A
  compromised server can write new backups, but **cannot read the old ones** — which symmetric
  encryption would not give us.
- The private key stays outside the infrastructure. Without it there is no restore: **the
  procedure has to be tested before this can be considered done**, because the security page
  claims that restore is tested.
- Done with [`test-restore.sh`](../deploy/backup/test-restore.sh): it spins up a disposable
  Postgres, runs the **real** `backup.sh` and `restore.sh`, and compares the content. It also
  checks what matters more than the restore succeeding — that the uploaded file **cannot be
  read**: it has the `age` header, `pg_restore` can't open it, no content text is inside it, and a
  different private key is rejected.

## 4. Phases

| # | What | Verification | Status |
|---|---|---|---|
| 1 | Encrypted format + `Put`/`Get`/`Remove` in `storage.Service`, with magic-byte detection | round-trip tests, and reading a legacy object | ✅ 10 tests |
| 2 | Block-based `GetSeek` | video seeking forward/backward; `Range` in the middle of a block | ✅ |
| 3 | Per-tenant key, versioned KEK | rotating the KEK without touching any objects | ✅ |
| 4 | Background sealing (old objects **and** composite recordings) | count: sealed objects = objects in the bucket | ✅ code; still needs to run in production |
| 5 | Tenant prefix on new recordings | a new recording lands under the tenant's key | ✅ |
| 6 | `age` on backups | **full restore from an encrypted backup** | ✅ automated rehearsal |
| 7 | Generate keys and verify in production | a new object in the bucket unreadable without our key | ⏳ |
| 8 | Update `/security`, `/sovereignty` and the Proton comparison | — | ⏳ |

## 5. What changes in the messaging

The R2 line goes from *"files, EU jurisdiction, US provider"* to *"the provider stores only
ciphertext; the key is ours and stays in the EU."* It's the cheapest way to strengthen the weakest
line on `/sovereignty` — cheaper than switching providers, which is the other option recorded in
the landing page plan (§2).

**What still can't be claimed:** that we cannot read the files. We can — we hold the keys, because
the product needs them to generate thumbnails, edit Office documents in the browser, and serve
public links. Saying otherwise would be exactly the mistake the sovereignty page exists to avoid.

## 6. Cost

- **Network:** no new cost. The traffic already goes entirely through the backend.
- **CPU:** hardware-accelerated AES-GCM is practically free next to the I/O.
- **Storage:** +0.0015% (16 bytes per MiB).
- **Latency:** no new query per read — the object's key travels inside the object (§2d) and the
  tenant's key is cached in memory, loaded once.

---

## 7. The mail gateway (`blobgw`)

Built on 2026-07-30, after §2f showed that mail was left out, and after checking Stalwart's
documentation confirmed that **its S3 storage backend has no encryption option at all** — the
fields are `bucket`, `region`, `accessKey`, `secretKey`, `securityToken`, `profile`, `timeout`,
`maxRetries`, `verifyAfterWrite`, `keyPrefix` and `allowInvalidCerts`. No SSE-S3, SSE-C or
customer key.

**The idea:** speak its language. Stalwart points its `endpoint` at
[`blobgw`](../backend/cmd/blobgw/) instead of Cloudflare; we encrypt with the format that already
exists and forward the request. It has no idea anything changed.

### Why it's small, and why it can run replicated

Having read its client (`crates/store/src/backend/s3/mod.rs`), it uses **four** path-style
operations: `GET`, `GET` with `Range`, whole-body `PUT`, `HEAD` (the `verify_after_write`) and
`DELETE`. **No multipart** — `put_object` receives the whole body at once.

That's what lets the service be **entirely stateless**, and therefore run in N replicas. It runs
in two: while one restarts, mail keeps being written through the other — which is what removes it
as a single point of failure on the mail path.

### What's left out, on purpose

Out-of-order multipart, pre-signed URLs, and listings — everything LiveKit needs for recordings.
Recordings keep using background sealing (§2e), which has a short, declared plaintext window. A
single piece serving two clients with different needs ends up serving both badly; once multipart
is built, it will be with the gateway already running.

### The signature is verified

"It's on a private network" is an assumption about tomorrow's infrastructure, not a property of
the service — and whoever talks to it reads all the mail. Because the SigV4 signature covers the
body's **declared** hash and not the body itself, the body is checked as it streams through, and
an object that doesn't match is deleted before a response is sent.

### What happens to old objects

The 43 blocks that were already sitting there in the clear still read fine: detection is by the
magic bytes in the header, and an object without them is served as-is. They stay unencrypted until
rewritten. Sealing them becomes safe **once the gateway is in the path** — which is why it isn't
automatic: sealing's allowlist still excludes `mail/`, and changing that without the gateway would
repeat the §2f outage.

### Status

| | |
|---|---|
| Code and tests | ✅ 11 tests, including a `PUT` signed like `rust-s3` and rejection of a swapped body |
| Rehearsal against production R2 | ✅ PUT, HEAD, GET, Range, encryption confirmed in the bytes, wrong signature rejected, DELETE |
| Service in production | ✅ `blobgw`, 2 replicas, port 8090, no public exposure |
| Stalwart pointed at it | ⏳ still needs to be changed under Settings → Storage → Blob store |
