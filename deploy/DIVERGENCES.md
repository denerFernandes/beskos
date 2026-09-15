# Divergences: brief (project.md) × real Stalwart v0.16.4

The brief (§11) says: *"If something conflicts with the reality of the Stalwart version, trust the real
version and flag the divergence."* This document is the **source of truth** for the Stalwart
integration. Verified empirically against the `stalwartlabs/stalwart:v0.16.4` image (booted locally) +
source code at tag `v0.16.4`.

---

## D1 — The REST Management API was REMOVED. There is no OpenAPI. (maximum impact)

The brief (§6/§7/§9) assumes REST endpoints (`POST /api/principal`, `POST /api/dkim`,
`GET /api/queue/messages`) and generating a Go client from an `openapi.yml`.

**Reality in 0.16:** the entire REST Management API was removed. Confirmed: every `/api/*` returns
**404**. There is no `openapi.yml` served or embedded. Administration is now **100% over JMAP**,
under the Stalwart-specific capability **`urn:stalwart:jmap`**, with methods in the **`x:`**
namespace.

**Consequence for this project:**
- ❌ The "generate a client via oapi-codegen" task is gone.
- ✅ We wrote a **hand-typed Go JMAP client** (`backend/internal/stalwart`).
- Standard envelope for every admin call:
  ```json
  { "using": ["urn:ietf:params:jmap:core","urn:stalwart:jmap"], "methodCalls": [ ["x:Object/fn", {…}, "c0"] ] }
  ```
- Endpoint: `POST /jmap/` · auth: Basic (global recovery admin) · `#creationId` refs work across calls.

Admin methods (registry): `x:Domain/{get,set,query}`, `x:Account/{get,set,query}`,
`x:DkimSignature/{get,set,query}`, `x:Bootstrap/{get,set}`, plus `x:Tenant`, `x:Role`,
`x:BeskosgList`, `x:OAuthClient`, `x:ApiKey`, `x:AppPassword`.

> Note: `Principal/*` (capability `urn:ietf:params:jmap:principals`) is NOT provisioning — these
> are JMAP's standard sharing/calendar principals. `Principal/set` to create a domain fails with
> `notRequest`.

---

## D2 — Recovery admin: `STALWART_RECOVERY_ADMIN`, not `[authentication.fallback-admin]`

The brief/initial config used `[authentication.fallback-admin]`. **Real:** env var
`STALWART_RECOVERY_ADMIN=admin:<password>`. That admin (accountId `d333333`) always works, including
after a restart. It's the global credential that only the Go backend uses.

---

## D3 — A BOOTSTRAP step is mandatory before any provisioning

A fresh server boots in **bootstrap mode**: any `x:` method (except `x:Bootstrap`) returns
`forbidden: "The server is in bootstrap mode…"`. The real flow (once per instance):

1. `x:Bootstrap/set` on the `"singleton"`:
   ```json
   {"using":["urn:ietf:params:jmap:core","urn:stalwart:jmap"],
    "methodCalls":[["x:Bootstrap/set",{"update":{"singleton":{
      "serverHostname":"mail.example.org","defaultDomain":"example.org",
      "requestTlsCertificate":false,"generateDkimKeys":true}}},"0"]]}
   ```
   The response returns the generated admin `admin@<domain>` + secret (**shown only once**).
2. **Restart the process** (`docker restart`) — the update only takes effect after a restart.

Only `serverHostname` + `defaultDomain` are required. `requestTlsCertificate:false` avoids
ACME/Let's Encrypt in development. → We model this as an idempotent control-plane setup step (not
the same thing as provisioning a tenant).

---

## D4 — Mapping the §6 operations to real JMAP (TESTED payloads)

### Create a domain (auto-generates DKIM) — `x:Domain/set`
```json
["x:Domain/set",{"create":{"d1":{"name":"customer.com","isEnabled":true,"description":"tenant_id=42"}}},"0"]
```
→ `{"created":{"d1":{"id":"c"}}}`. The default `dkimManagement` `{"@type":"Automatic"}` generates
Ed25519+RSA on the spot. We store the returned `id` in `domains.stalwart_id`.

### Create an individual mailbox — `x:Account/set`
Address = `name` + `domainId` (there is NO `email` field). `credentials`/`aliases`/`quotas` are
**maps** (`{"0":…}`), not arrays.

#### D4b — `aliases` is a map of OBJECTS, not of addresses

Confirmed in the source (`crates/registry/src/schema/structs.rs`): `UserAccount.aliases` is
`List<EmailAlias>`, and `EmailAlias` is `{enabled, name, domainId, description}`. In JSON,
`List<T>` serializes as an indexed map — so the correct value is:

```json
["x:Account/set",{"update":{"<accountId>":{"aliases":{
  "0":{"name":"noreply","domainId":"<the domain's Stalwart id>","enabled":true}}}}},"0"]
```

An alias is described **like a mailbox** (local part + `domainId`, no `email` field), just like the
account itself. Sending strings — `{"0":"noreply@domain.com"}` or `["noreply@domain.com"]` — gives
`invalidPatch: "Invalid value type for object", properties:["aliases"]` (caught in production,
2026-07-28). And `enabled` can't be missing: it defaults to `false`, which would leave the alias
existing without ever delivering.

See `SetAccountAliases` in `internal/stalwart/provisioning.go`.
```json
["x:Account/set",{"create":{"u1":{
  "@type":"User","name":"john","domainId":"c","description":"John",
  "credentials":{"0":{"@type":"Password","secret":"<strong-password>"}},
  "quotas":{"maxDiskQuota":5368709120}}}},"0"]
```
→ `{"created":{"u1":{"id":"c"}}}`; `emailAddress` resolves to `john@customer.com`. We store this in
`mail_users.stalwart_account_id`. `domainId` accepts either a live id (`"c"`) or a same-request ref
(`"#d1"`) → so a domain + mailbox can be created in a single round trip.
⚠️ The password goes through a strength checker (zxcvbn); weak passwords → `invalidProperties`.

### DKIM — `x:DkimSignature`
Recommended: leave `dkimManagement` on Automatic (the default). To read the keys for building DNS:
`x:DkimSignature/query` → `/get` (returns `selector`, `publicKey`, `@type`, `stage`). Manual
creation requires supplying `privateKey.secret` (PEM).

### Read/edit/remove — `x:Account/set` / `x:Domain/set`
- Edit (JSON pointer into subproperties):
  `{"update":{"c":{"quotas/maxDiskQuota":2147483648,"credentials/0/secret":"<new>"}}}`
- Remove: `{"destroy":["c"]}`. **Referential integrity:** destroying a domain with linked
  DKIM/accounts fails with `objectIsLinked`. Correct order: accounts → the domain's
  DkimSignatures → the domain.

### Suspending an account — **there is no "active" field** (2026-08-01)

An account in 0.16 carries `name`, `domainId`, `credentials`, `roles`, `permissions`, `quotas`,
`aliases`, `description`, `locale`, `encryptionAtRest`, `usedDiskQuota`, `emailAddress` — and
**nothing** to switch it off. Suspending is done by **removing the credentials**:

```json
["x:Account/set",{"update":{"<id>":{"credentials":{}}}},"c0"]   // suspend
["x:Account/set",{"update":{"<id>":{"credentials/0/secret":"<new>"}}},"c0"]  // reactivate
```

Verified against a live account: the next login attempt is refused **immediately** (no caching in
between), and writing a new secret restores access just as immediately. The mailbox keeps
everything and mail keeps being delivered — it's a suspension, not a deletion.

⚠️ **`credentials` does not accept a partial patch.** Writing `credentials/0/expiresAt`
**destroys the entire credential**, and writing it `null` leaves `credentials: {}` — discovered
this the hard way on a test account that ended up with no way in. Either write
`credentials/0/secret` (which recreates it) or replace the whole map. There is no middle ground,
and the `expiresAt` the object shows does **not** work for suspension.

Design consequence: **reactivating requires a new password**. See `SuspendAccount` in
`internal/stalwart/provisioning.go`.

### DNS records for the customer to publish — the `dnsZoneFile` property
```json
["x:Domain/get",{"ids":["c"],"properties":["dnsZoneFile"]},"0"]
```
⚠️ **DKIM timing (verified in the integration test):** on a **freshly created** domain, the DKIM
TXT record (`_domainkey`) does **not** appear immediately in `dnsZoneFile` — only SPF/MX/DMARC/
MTA-STS/SRV are there right away. Signature generation isn't synchronous with the create. → The
DNS screen (`dns_pending`) should **poll** the zonefile until DKIM shows up before treating the
record list as complete.

Returns a ready BIND zone snippet with **every** record the server computed: DKIM TXT (per
selector), SPF (`v=spf1 mx -all`), `MX 10 mail.<domain>.`, `_dmarc` (`p=reject`), MTA-STS, TLS-RPT,
autoconfig/autodiscover, SRV (imaps/submissions/jmap/…). Default `dnsManagement` is `Manual` (the
server reports, the customer publishes). → This is the source for the `dns_pending` DNS screen
(§7). Parse this zonefile instead of building records by hand.

---

## D5 — No provisioning `stalwart` CLI in this image

The `stalwart` binary is server-only (`--config/--export/--import/--console`). There is no
`stalwart-cli`/`apply`. This confirms the choice to integrate over JMAP/HTTP (more testable, no
coupling to the binary).

---

## D6 — Impact on the provisioning state machine (§7)

The mapping still holds, with the calls swapped in:
```
pending → domain_created (x:Domain/set) → dkim_created (automatic on create; just read it for DNS)
        → dns_pending (read dnsZoneFile) → dns_verified (our own DNS polling) → active (x:Account/set)
```
`dkim_created` stops being a separate call once `dkimManagement=Automatic` — it becomes "read the
generated signatures".

---

## D7 — Webmail (Phase 2): no CORS, no password grant, `.test` breaks sending

Verified against real Stalwart 0.16.4 (end user `alice@wm.test`):

- **CORS doesn't exist and isn't configurable.** A preflight `OPTIONS /jmap/` answers 204 with
  **no** `Access-Control-Allow-*` header; POSTs with a cross-site `Origin` return 200 but the
  browser blocks reading the response. The official docs only cover IP/listener restriction. →
  **React never talks to Stalwart directly.** **Mandatory architecture: a same-origin reverse
  proxy** (nginx/Caddy/Vite proxy) serving the SPA and forwarding `/jmap`, `/jmap/download`,
  `/jmap/upload`, `/auth`, `/.well-known` to Stalwart.
- **End-user auth:** Basic auth (`email:password`) against `/jmap/session` **works** (200, returns
  the user's accountId). OAuth 2.0 exists (`authorization_code`+PKCE S256, `device_code`) but
  **there is no `password`/`client_credentials` grant** — there's no way to exchange a
  user+password for a Bearer token without interactive consent. The brief's (§8) `POST /api/auth`
  (code→token) is **gone** (404). → Phase 2: **Basic auth / app-password behind the proxy**;
  interactive OAuth is left for unified login later.
- **Session URLs** (`apiUrl`, `downloadUrl`, `uploadUrl`) use the configured `serverHostname`, not
  the request origin. Trust the **paths** (`/jmap/`, `/jmap/download/{accountId}/{blobId}/{name}`),
  not the hosts.
- **The `.test`/`.local` TLD breaks SENDING:** `Identity/get` comes back empty (public-suffix
  validation fails), which blocks `EmailSubmission`. Reading/auth still work. → Always provision
  with a **real public TLD**.
- **JMAP flows tested:** `Mailbox/get` (use `role`, ids are opaque); `Email/query`+`Email/get` with
  the `#ids` back-reference; reading with `fetchTextBodyValues`/`fetchHTMLBodyValues`; sending =
  `Email/set` (draft, `mailboxIds` with the real Drafts id, `identityId` required from
  `Identity/get`) + `EmailSubmission/set` with `onSuccessDestroyEmail`; search via `filter.text`.

## D8 — Webmail: brokering the JMAP credential (hardening H11, 2026-07)

A direct consequence of D7 (no password grant; React never talks to Stalwart directly). Before
this, the webmail stored the Basic credential (`base64(email:password)`) in `localStorage` and sent
it on every JMAP request — a reversible password exposed to any XSS. Since Stalwart 0.16.4 offers
no non-interactive bearer/app-password exchangeable for a password (D7), we adopted the
**BFF (backend-for-frontend)** pattern:

- On login (`/api/webmail/login`), the backend verifies the password against Stalwart and **stores
  the Basic credential ENCRYPTED** (AES-256-GCM, `internal/secretbox`, key `MFA_ENC_KEY`) in
  `mail_users.jmap_cred`.
- **All** JMAP traffic goes through our own proxy (`/jmap`, `/auth`, `/.well-known` → the Go API,
  **not** straight to Stalwart). The proxy authenticates via the **session** (an HttpOnly cookie on
  the web, an opaque Bearer token in the native app) and **injects** the mailbox's Basic credential
  server-to-server (`jmap_proxy.go` → `jmapBrokerAuth`).
- The browser **never** holds the mailbox password. It only persists non-secret data
  (email/accountId) plus the opaque, revocable session token, and only in the native app; on the
  web the session is the HttpOnly cookie.

Operational implications:
- **Deploy:** the reverse proxy (Caddy/nginx/Vite) routes `/jmap`, `/auth`, `/.well-known` to the
  **Go API** (`:8000`), no longer to Stalwart. The API reaches Stalwart over the internal network
  (`STALWART_URL`).
- **`MFA_ENC_KEY` is required in production** (the config fails closed without it) — it encrypts
  the JMAP credential and the TOTP secret. Rotating the key invalidates every stored credential
  (users have to log in again).
- **Login expires** (session token, 7 days): with no stored password, the user just logs in again
  on expiry — the old "eternal login," which was exactly the risk vector, is gone.
- Sessions from before this migration (empty `jmap_cred`) get a 401 on JMAP → re-login (which
  populates the credential).
- **Verification:** needs an end-to-end integration test against the full stack (Stalwart + API +
  proxy). The brokering only uses Basic auth (proven in D7); the risk is in the wiring, not in
  Stalwart.

## White-labeling outbound mail (SES) — custom MAIL FROM (2026-07-25)
Gmail (and similar clients) show **"sent by" = the envelope MAIL FROM (Return-Path) domain**.
Without a custom MAIL FROM on the SES identity, that domain is the default
`<region>.amazonses.com` → **every recipient saw Amazon**, even though the DKIM ("signed by") was
already `beskos.com`.

**Decision:** configure a custom MAIL FROM `bounce.<domain>` per tenant (`ses.SetMailFrom`,
`BehaviorOnMxFailure=USE_DEFAULT_VALUE` so sending doesn't stall before the MX propagates).
`AugmentZoneForSES` now emits, on the bounce subdomain,
`MX 10 feedback-smtp.<region>.amazonses.com` + `TXT "v=spf1 include:amazonses.com ~all"`.

**Trade-off accepted:** this is the **only** place in the customer's DNS that references
amazonses, and only on the `bounce.<domain>` subdomain (visible only to someone who runs `dig`
against that subdomain). The apex domain's own records stay entirely Amazon-free (SPF via
`_spf.<brand>`, DKIM from our own key, DMARC). We gain the white-label appearance that **every
recipient** sees, at the cost of one technical bounce MX. Rejected alternative (no custom MAIL
FROM): fully clean DNS but `mailed-by: amazonses` for everyone. See `provisioning/zone.go`,
`ses/ses.go`, tests `zone_ses_test.go`/`machine_test.go`.

## Our own branding in the office editor — a patch to Document Server (2026-07-27)

Until now the stance was "official image, unmodified, network only" (same as Stalwart). **That
stops applying to OnlyOffice** — and only to it. Stalwart stays untouched (law §5.4).

**Measured reality** (pinned image, `onlyoffice-documentserver 9.4.0-129`, arm64):
- The official docs mark `customization.logo` and `customization.customer` as exclusive to the
  **Developer Edition**. Confirmed empirically: on Community the server reports the license with
  `branding: false` and `customization: false`, and the client **silently drops the
  parameters** — no error, no warning.
- The gate is **two lines per editor**, in
  `web-apps/apps/<editor>/main/app/controller/Main.js`:
  ```js
  this.appOptions.canBrandingExt = params.asc_getCanBranding() && (...)   // layout, loaderLogo, about
  this.appOptions.canBranding    = params.asc_getCustomization()          // header logo + "About"
  ```
  (Note the inversion: `asc_getCanBranding` feeds `Ext`, not the base one.)
- The 9.4 `web-apps` ships **unminified** — which is why the patch fits in a `sed`. The 9.4
  `DocService`, by contrast, is a single ~263 MB ELF binary with the JS embedded: patching the
  server would be far more expensive, and unnecessary, because the license verdict is only
  *applied* on the client.
- The DS's nginx serves with `gzip_static`: without regenerating the `.gz` alongside it, the
  browser gets the original file and the patch looks like it had no effect. This is the easy
  mistake with this modification.
- **The logo CANNOT be a `data:` URI.** The DS's `api.js` passes `customization.logo.{image,
  imageDark,imageLight}` in the editor iframe's **query string** (lines ~1216-1219). Three ~7 KB
  data URIs, percent-encoded, add up to a ~57 KB request line → **`414 Request-URI Too Large`**
  when opening any document. Fix: the patch copies the SVGs into the DS's own webroot
  (`web-apps/apps/common/main/resources/img/beskos/`) and the backend sends the URL instead — 362
  characters, same origin as the editor, no CORS and no dependency on the app being publicly
  reachable.

**Decision:** apply the patch at container startup (`deploy/onlyoffice-branding-patch.sh`, called
by the entrypoint), keeping the official image pinned by digest. Idempotent and fail-safe: if the
pattern changes in a future DS version, the script warns and the editor still starts, just without
the branding.

**Compliance (AGPL v3 + Ascensio System SIA's trademark policy):**
- The 9.4 additional terms no longer require keeping the ONLYOFFICE logo in modified versions'
  UI — the old *badgeware* clause is gone; the file header points to **section 5** of the AGPL
  ("Appropriate Legal Notices") and says "No trademark rights are granted under this License".
- What's still required, and what we do: a **dated** modified-version notice, identification of
  the original software, a link to the license, and the offer of the modified source (§13,
  because we serve it over the network). All of this goes in
  `customization.customer.info` (see `backend/internal/httpapi/onlyoffice_branding.go`), and the
  DS's own `setLicInfo` keeps the *"Powered by / Ascensio System SIA / onlyoffice.com"* block in
  the "About" panel whenever we send `customer`.
- `ONLYOFFICE_SOURCE_URL` is **required in production** (the config refuses to boot without it) —
  it's the address of the modified source. Point it at your own fork before running this in
  production.
- Note: their *commercial FAQ* still says removing the branding requires a paid license, which
  contradicts the terms of the LICENSE itself. The license is what binds; the residual risk is a
  dispute, not a clear breach. This decision was made with that risk in view.

**Color palette (2026-07-27):** the tab layout is generic, but color is identity — their trademark
policy forbids imitating their visual identity, and an editor that looks identical to the official
images is exactly what we don't want. We swapped their blue (`#446995`) for Beskos's own teal. Two
measured pitfalls:

- **A custom theme via `themes.json` does NOT work at startup.** The one serving themes is
  DocService (not nginx): it reads files from `services.CoAuthoring.themes.uri` and publishes them
  at `GET /themes.json`, **wrapping each file in a `themes` array** — so each file has to contain
  ONE theme, not a list, or it ends up nested one level too deep and is silently ignored. Even past
  that: the editor applies the class (`body class="theme-beskos"`) but **never writes the CSS
  variables**, because `write_theme_css()` is commented out in `parse_themes_object` and only runs
  inside `apply_theme()` — i.e. only when the user actually SWITCHES theme from the menu. Measured:
  `--toolbar-header-document` stayed `#446995` with the Beskos theme active.
- **Fix:** the patch appends `beskos-theme.css` to the end of each editor's `app.css`, targeting
  `:root`/`.theme-type-light` and `.theme-dark`/`.theme-type-dark` — it wins on source order at
  equal specificity, applies no matter which theme the user picks, and doesn't depend on their
  theme mechanism at all. `uiTheme` goes back to the standard `theme-light`/`theme-dark`.

**Verification:** `docker compose logs onlyoffice | grep branding` → `5 editor(s) modified`; and
`curl -sL -H 'Accept-Encoding: gzip' --compressed http://127.0.0.1:8082/web-apps/apps/documenteditor/main/app/controller/Main.js | grep beskos-branding-patch`
has to find the marker (this is the test that catches a stale `.gz`).

## D9 — 0.16.4's configuration is JSON and lives in the registry, not in `config.toml` (2026-07-29)

**Discovered while preparing the Postgres migration for mail.** Proven against the official
`stalwartlabs/stalwart:v0.16.4` image, locally.

This repository's `deploy/stalwart/config.toml` **is not the format this version reads**:

1. The image's default command is `stalwart --config /etc/stalwart/config.json` — our file was
   mounted at `/opt/stalwart/etc/config.toml`, which is **never read**.
2. That file is no longer the whole configuration. It's just the **data store** definition;
   listeners, TLS, domains, authentication and everything else moved into a **registry** kept
   inside that store and edited through the admin interface.
3. With no file at all, 0.16.4 boots in **ephemeral-storage bootstrap mode** — it writes nothing to
   disk (confirmed: `/opt/stalwart` never even comes into existence).

**Practical consequence, and a serious one:** the SES relay block commented out in our
`config.toml`, with instructions saying "uncomment for production", **would have done absolutely
nothing**. The same goes for any other tuning someone might think they're doing there.

**Proven startup file format** (booted and came up healthy, with 27 tables created):

```json
{
  "@type": "PostgreSql",
  "host": "postgres", "port": 5432, "database": "stalwart",
  "authUsername": "beskos",
  "authSecret": { "@type": "Value", "secret": "..." }
}
```

Two pitfalls found along the way:

- `@type` is `PostgreSql` (the accepted variants are `RocksDb`, `Sqlite`, `FoundationDb`,
  `PostgreSql`, `MySql`) — lowercase `postgresql` is rejected.
- The secret is a tagged enum: `{"@type":"Value","secret":"..."}`. Writing `"value"` instead of
  `"secret"` **gives no error** — it gives an empty password and an obscure Postgres
  authentication failure. The other variants are `None`, `EnvironmentVariable` and `File`.

**Confirmed in production (2026-07-29), with access to VPS-1:**

- The platform does **not** run via `docker compose` — these are **swarm services**. This
  repository's `deploy/docker-compose.yml` also doesn't describe what's actually running there.
- The `stalwart` service boots with `--config /etc/stalwart/config.json`, mounted from
  **`/opt/beskos/stalwart/config.json`** on the host.
- That file has exactly the format above — and **already points at PostgreSQL**: database
  `stalwart`, on the same server as the application. In other words, mail messages, attachments,
  and accounts are all in SQL, and exportable hot (which is what the backup service has been doing
  ever since).

**What to do with `deploy/stalwart/config.toml`:** remove it, or replace it with the real file.
As long as it sits there, with comments saying "uncomment for production", it will keep misleading
whoever reads it — which is exactly what happened, and led to planning a migration that wasn't
actually needed.


## How to reproduce / re-verify
Test container: `docker run -d --name beskos-sw-probe -e STALWART_RECOVERY_ADMIN='admin:probe-secret' -p 18080:8080 stalwartlabs/stalwart:v0.16.4`
Auth: `admin:probe-secret`. See `backend/internal/stalwart/*_test.go` (integration tests that
exercise these payloads).
