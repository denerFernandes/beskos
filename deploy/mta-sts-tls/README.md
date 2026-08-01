# MTA-STS policy server (on-demand TLS)

Serves `https://mta-sts.<domain>/.well-known/mta-sts.txt` for **every** customer domain, with a
certificate issued per domain the first time a sender asks for it.

## Why it exists

MTA-STS is the record that tells other mail servers "always deliver to me over TLS, and only to
these hosts". Publishing it is the difference between a customer scoring 10/11 and 11/11 on our own
audit — and, less cosmetically, it is what stops a downgrade attack on their mail.

The catch is that the policy has to be fetched over HTTPS from a host named after the *customer's*
domain. So there is a certificate per customer domain, and it can only be issued when the first
sender arrives. That is what Caddy's on-demand TLS does.

## Why it does not have CapRover's credentials

Caddy asks our API — `GET /api/public/mta-sts-allowed?domain=<host>` — whether a hostname belongs to
a customer before issuing anything. The authority runs from Caddy to us, never the other way round:
the backend never gains the ability to reconfigure the platform. Without that gate, anybody could
point a DNS record at this address and have us mint certificates until the issuance limits lock us
out for every real customer.

## Why it is not in deploy.py

CapRover reapplies the stored app definition on every publish. This app's definition asks for port
80, which CapRover's own nginx already holds in host mode, so the publish fails and leaves the
service down. The definition cannot be corrected either: CapRover's `appDefinitions/update`
endpoint answers HTTP 500 for this app, with an empty body, for any payload including an exact echo
of what it already stores.

So the service is driven directly through Docker Swarm. Publishing a change:

    docker buildx build --platform linux/amd64 \
      -t ghcr.io/denerfernandes/img-captain-mta-sts-tls:<n> --push deploy/mta-sts-tls

    ssh deploy@<leader> docker service update \
      --image ghcr.io/denerfernandes/img-captain-mta-sts-tls:<n> srv-captain--mta-sts-tls

`--update-order` must stay `stop-first` (the default): the published port is in host mode, so two
tasks cannot hold 443 at the same time and `start-first` deadlocks the update.

## Where it runs

Pinned to **vps2** (node `h7vnxjrmbjia2g8r97hlnsh4a`, 185.232.71.20) by a Swarm constraint.

- Not the leader: CapRover's nginx binds 80 and 443 there, in host mode.
- Not vps3: LiveKit's own Caddy already binds 443 there.

`mta-sts.beskos.com` is an A record to that address, and every customer's `mta-sts.<domain>` is a
CNAME to `mta-sts.beskos.com` (written by `provisioning.PublicHosts`). Moving the service to another
node means changing that one A record — the customer CNAMEs follow.

Certificates live in the `mta-sts-tls-data` volume. Because it is a local volume on a pinned node,
moving the service to a different node starts issuance from scratch.

## One policy for everyone

`mta-sts.txt` is a single static file naming `mail.beskos.com`, which is the MX of every customer
domain. That is what keeps this a four-line service instead of a file per customer.

If customers are ever pointed at different mail hosts, this stops being correct: the policy would
have to be generated per domain, and a wrong `mx:` line makes receivers **refuse** the mail rather
than fall back.
