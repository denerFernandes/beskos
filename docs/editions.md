# Community and Pro

## License

The core is [Business Source License 1.1](../LICENSE.md): free to run in production for your own
organization, including through an integrator you pay for setup and support, with no seat limit.
What it doesn't allow is offering it to third parties as a hosted or embedded service -- that
needs a commercial license (see [LICENSE.md](../LICENSE.md#what-this-means-in-practice) for a
plain-language table of what's allowed). Every released version converts to Apache License 2.0
four years after its release, so nothing here is ever permanently locked away. `ee/` directories
carry a separate [Enterprise license](../ee/LICENSE) instead, usable only with a valid Pro key.

## What's Pro

Pro is **€1 per user per month**, with everything else -- including every security feature --
free in Community:

| Community | Pro |
|---|---|
| Email, calendar, contacts, Drive, chat, wiki, Kanban | Meeting recording (LiveKit Egress + offline compose) |
| Video meetings, without recording | White-label branding: your logo, colors, domain, no "Powered by" |
| Office editing (OnlyOffice) | Audit log and retention policy UI *(planned)* |
| AI assistant, with your own provider key | Organization-wide security policies (mandatory MFA, session limits) |
| RBAC, groups, invitations, MFA (TOTP/SMS) | AI agents (org-wide automations) |
| IMAP and file migration | Bulk-assisted migration across several domains *(planned)* |
| Desktop app and folder sync | Priority support, SLA |
| pt-BR / en | |

The rule of thumb: a feature stays Pro when a business would pay for it without hesitation and a
small team or an individual wouldn't miss it. Nothing that affects basic security is ever Pro.

*(planned)* marks two rows above that are not implemented yet. Of the rest, white-label branding,
organization-wide security policies, and AI agents are built and enforced server-side in
`backend/ee/`: a Community build does not link that code at all, independent of whether a
licensing portal exists to actually sell a key yet. Meeting recording is the one exception --
its code ships in every build and is gated at runtime by the license key's `Recording`
capability (`internal/edition`), not by the `pro` build tag; see
[architecture.md](architecture.md#the-ee-seam) for why.

## Offering Beskos as a service

The Business Source License's [Additional Use Grant](../LICENSE.md) exists precisely to prevent
this without a separate agreement -- reselling it as a hosted or embedded service needs a
commercial license, priced by revenue share or by active seat depending on the arrangement. Get
in touch at <https://beskos.com/licensing>.
