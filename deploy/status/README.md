# Status page — status.beskos.com

Public, no authentication: <https://status.beskos.com>. It is the measurement behind the
availability commitment — without a number customers can see, "99.5%" is only our word against
their memory. It also serves the subprocessor notices (it is a single subscription list).

## What is deployed

**Uptime Kuma** (`louislam/uptime-kuma:1`), `status` app on CapRover:

| Setting | Value | Why |
|---|---|---|
| Port | 3001 | Kuma's port; without it CapRover routes to 80 and returns 502 |
| Volume | `status-data` → `/app/data` | History lives in SQLite. Without a volume, every deploy wipes the 90 days the page exists to show |
| WebSocket | enabled | Kuma speaks socket.io; without it the handshake returns 400 and the interface does not even open |
| TLS | subdomain certificate, `forceSsl` | `beskos.com` is CapRover's root domain, so `status.beskos.com` is not a "custom domain" — `enablebasedomainssl` is used |
| DNS | A → 185.232.71.70, **not proxied** | CapRover's Let's Encrypt validation is HTTP-01 and must reach the origin |

**Subdomain rather than `/status`:** if the main ingress goes down, a `/status` served from the same
place goes down with it — exactly when people look for it. `beskos.com/status` redirects here
(307, deliberately temporary: a 308 is cached by the browser forever).

## Probes

Ten, every minute, from the outside. Defined in [`kuma-setup.py`](kuma-setup.py) — running the
script again does not duplicate anything.

The page is **in English** — it is public, read by customers in several countries and cited in
conversations with auditors. (Kuma's frame — "All Systems Operational", "Last Updated" — follows
the visitor's browser language; 1.x has no way to pin it server-side.)

The **operator console** is measured but **does not appear** on the public page: listing an
internal panel tells the world that it exists and where.

Two probes are worth noting:

- **API** points to `/api/readyz`, which touches the database, and not to `/healthz`. It also has
  to be under `/api`: the public proxy only forwards that prefix to the API — at the root it would
  measure the static file server and report "up" with the database down.
- **Mail — read (JMAP)** accepts **401**. That is the correct response from an endpoint that
  requires authentication, and it proves Stalwart answered; requiring 2xx would keep the row red
  forever.

## Running the setup

```sh
pip install "python-socketio[client]"
python3 deploy/status/kuma-setup.py
```

Credentials are in `deploy/secrets.env` (`STATUS_ADMIN_USER`, `STATUS_ADMIN_PASSWORD`). The admin
panel is at <https://status.beskos.com/dashboard>.

## Still missing

- **Alerts** (email/webhook) when a probe goes down — Kuma supports it; it is not configured yet.
- **Deep probes**: send an end-to-end test email and measure how long it takes to arrive; write and
  read an object in storage. Today's probes prove the service responds, not that the full job works.
- **Separate VPS.** For now the page runs on the same infrastructure it measures: in a total outage
  it becomes unreachable. That is less bad than a page claiming everything is fine while it is not,
  but it is to be replaced.
