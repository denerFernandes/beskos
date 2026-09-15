# Getting started

## Requirements

Docker and Docker Compose (v2), and ports 25/143/465/587/993 free on the host if you want to
send and receive real mail -- most cloud providers block outbound 25 by default, so mail sending
from a fresh VM often needs that unblocked first.

## Run it

```bash
git clone https://github.com/beskosorg/beskos.git
cd beskos
cp deploy/.env.example deploy/.env
```

Edit `deploy/.env`: at minimum, set real values for `POSTGRES_PASSWORD`,
`STALWART_ADMIN_SECRET`, and `MINIO_ROOT_PASSWORD`. Everything else has a working default for a
local trial. See [docs/configuration.md](configuration.md) for what every variable does.

```bash
docker compose -f deploy/docker-compose.yml --env-file deploy/.env up -d
```

The `api` container applies pending database migrations itself on startup -- there is nothing to
run by hand for that. Give everything a minute to become healthy, then run the one-time Stalwart
bootstrap (a fresh Stalwart refuses every administrative call until this runs once):

```bash
cd backend
STALWART_URL=http://localhost:8080 STALWART_ADMIN_SECRET=<STALWART_ADMIN_SECRET> \
  STALWART_HOSTNAME=mail.example.org STALWART_DEFAULT_DOMAIN=example.org \
  go run ./cmd/bootstrap
```

`bootstrap` prints a generated Stalwart admin address and secret once -- save it; the running
server needs a restart to pick the bootstrap up:

```bash
docker compose -f deploy/docker-compose.yml restart stalwart
```

## First login

Open the frontend (in dev, `npm run dev` inside `frontend/`; in this compose, whatever reverse
proxy you put in front of `api`'s port 8000 -- see [docs/self-hosting.md](self-hosting.md) for a
production Caddy config) and sign up. The first organization on a fresh database becomes its own
tenant automatically; there is no separate "create the first admin" step.

## What's next

- [docs/architecture.md](architecture.md) for how the pieces fit together.
- [docs/self-hosting.md](self-hosting.md) for TLS, backups, and running this in production.
- [docs/configuration.md](configuration.md) for every environment variable, including the
  optional ones (AI assistant, video meetings, office editing, SMS for MFA).

## Something not working?

Check the container logs first: `docker compose -f deploy/docker-compose.yml logs -f
api worker stalwart`. A domain stuck in `dns_pending` almost always means the DNS records
`GET /api/domains/{id}/dns/check` lists haven't propagated yet -- for local testing without real
DNS, set `PROVISION_SKIP_DNS_VERIFY=true`. For anything else, open an issue with what you ran and
what you expected instead.
