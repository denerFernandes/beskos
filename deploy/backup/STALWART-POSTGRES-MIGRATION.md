# Mail on Postgres — already done

**There is no migration to do.** Verified in production on 2026-07-29: Stalwart already runs on
PostgreSQL — database `stalwart`, on the same server as the application, with its messages,
attachments (4,928 blobs), accounts and configuration all in SQL.

This document used to plan a migration from RocksDB, downtime window included. It was written from
this repository's `deploy/stalwart/config.toml` — which, as it turned out, **was never read by
production**. See [DIVERGENCES.md](../DIVERGENCES.md) §D9.

The lesson is worth more than the plan: **a plan written against the repository's configuration is
not a plan about the system.** The first step should have been to look at the machine.

## What still holds

- Mail is exported three times a day, hot, along with everything else — see [README.md](README.md).
- The actual configuration is at `/opt/beskos/stalwart/config.json` on VPS-1, mounted into the
  `stalwart` swarm service. This repository's `config.toml` **has been removed** and replaced by
  the real format (`deploy/stalwart/config.json`), so no one relies on it again.
- Attachment storage remains inside Postgres. Moving it to R2 (`blob`) would relieve the database
  and the backup, and is an option to consider as volume grows — not urgent at 12 MB.
