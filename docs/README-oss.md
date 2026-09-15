<!-- This file becomes README.md in the public export (scripts/export_public.py). It stays here,
     separate from the internal README.md, because that one still documents day-to-day status for
     the team; see docs/OPEN-SOURCE.md §3.1. -->

# Beskos

A self-hosted email, calendar, files, chat, and knowledge-base platform for a team that wants to
stop paying for five different SaaS subscriptions and run one thing instead. Email runs on
[Stalwart](https://stalw.art) underneath, used over the network only, never modified; everything
else -- webmail, Drive, wiki, Kanban, video meetings, an AI assistant -- is built around it.

## What's in the box

- **Email**: full IMAP/JMAP/SMTP via Stalwart, a modern webmail, per-domain provisioning, DKIM/SPF/DMARC handled for you.
- **Calendar & meetings**: month/week/day views, shared calendars, conflict warnings, and video calls (LiveKit) with scheduling and RSVPs.
- **Files**: a Drive with folders, sharing, and in-browser editing of Word/Excel/PowerPoint documents (OnlyOffice).
- **Chat & Kanban**: real-time channels and a lightweight board, in the same app as your email.
- **Wiki**: a Confluence-style knowledge base with access by group or person, and AI-powered search.
- **AI assistant**: summarize, draft, and act on your mail and documents, using an AI provider you configure.
- **Security**: RBAC with per-member permission overrides, invites, MFA (TOTP and SMS), audit-friendly logging.
- **A native app**: desktop and mobile builds (Tauri) with two-way folder sync to the Drive.

See [docs/FUNCTIONALITY.md](FUNCTIONALITY.md) for the full, current list.

## Community and Pro

Beskos is source-available under the [Business Source License 1.1](../LICENSE.md): free to run,
for your own organization, on your own infrastructure, with no seat limit. What it does not
allow is reselling it as a hosted service to third parties -- that needs a
[commercial license](../LICENSE.md#121-licenciamento-dual-o-servico-concorrente-compra-a-licenca-comercial).
Every BUSL-licensed version converts to Apache 2.0 four years after its release, so nothing here
is ever permanently locked away.

A handful of features that a business pays for without hesitation -- meeting recording,
white-label branding, an audit log, organization-wide security policies, AI agents -- are Pro,
at **€1 per user per month**. Everything else, including every security feature, is Community.
See [docs/OPEN-SOURCE.md §3.2](OPEN-SOURCE.md) for the full split.

## Quickstart

```bash
git clone https://github.com/beskosorg/beskos.git
cd beskos
cp deploy/.env.example deploy/.env   # fill in the values you need
docker compose -f deploy/docker-compose.yml up -d
```

Then open the app at the address `PUBLIC_URL` points to, and follow
[docs/getting-started.md](getting-started.md) to add your first domain. Production deployment,
backups, and scaling live in [docs/self-hosting.md](self-hosting.md).

## Architecture, in one paragraph

A Go control plane (`backend/cmd/api`, `backend/cmd/worker`) talks to Stalwart over JMAP for
everything mail-related, and to its own Postgres for everything else -- domains, users, files,
chat, wiki, calendar. The React frontend (`frontend/`) never holds a Stalwart admin credential;
it authenticates as the end user and reaches Stalwart only through a same-origin proxy. See
[docs/architecture.md](architecture.md) for the full picture, including why JMAP and not the
REST API the Stalwart docs describe (short version: the pinned Stalwart version doesn't ship
one -- see [deploy/DIVERGENCES.md](../deploy/DIVERGENCES.md)).

## Contributing

Bug reports, features, and pull requests are welcome -- see [CONTRIBUTING.md](../CONTRIBUTING.md)
for the development setup and the rules the codebase follows. Security issues go to
[SECURITY.md](../SECURITY.md)'s address, not a public issue.

## License

[Business Source License 1.1](../LICENSE.md) for the core, converting to Apache 2.0 four years
after each release. The `ee/` directories carry the [Beskos Enterprise License](../ee/LICENSE)
instead. "Beskos" and its logo are trademarks -- see [TRADEMARK.md](../TRADEMARK.md).
