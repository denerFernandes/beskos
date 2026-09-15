# Roadmap — Identity, Admin in the menu, Groups, and Contact CRM

Large epic, broken into phases (law §6.1: one phase at a time, tested before the next).
The decisions below were made with the customer — they're fixed; changing them needs a new
conversation.

## Decisions

| Topic | Decision | Consequence |
|---|---|---|
| **Identity** | **Unify: kill `panel_users`** | A single identity (`mail_users`) for panel and webmail. Data migration + refactor login/MFA/sessions. The epic's biggest risk. |
| **Company × contact** | **Separate `companies` table** | `contacts.company_id` → sub-contacts with a job title. Search/tags/sharing need to cover both entities. |
| **Tickets** | **Full ticketing system** | Status, priority, queue, assignee, reply thread, SLA. Its own subsystem (phase 6). |
| **Customer tasks** | **Reuse the Kanban** | `cards.contact_id`. A single notion of "task" in the product; reuses AI, filters, assignee, and due date. |

## Model review (2026-07-15) — "we're mixing concepts"

Diagnosis: `contacts` served three owners with opposite rules — an address book (mine, private), a
customer (the company's, shared), and support (the team's). The symptom that showed up in testing:
a ticket visible to everyone with `contact_name` set to `null` → "Ticket #12 from —". The model was
contradicting itself out loud.

Customer decisions:

| Topic | Decision | Consequence |
|---|---|---|
| **Address book × Customer** | **Two entities** | `contacts` goes back to being a personal address book (always private, inside E-mail). `clients` is born (kind = company \| person) + `client_contacts`. |
| **Visibility** | **Cut the "groups" level** | Address book = private; customer = the company's. `contact_shares`, `company_shares`, the selector, and the visibility subquery repeated in every read all go away. Zero shares existed — the migration loses nothing. |
| **Support** | **Inside Clients** | No icon of its own; a ticket always belongs to a customer and lives on their page. |

The commercial angle driving the cut: the competitor is **Google Workspace + 2 SaaS products**,
not Zendesk. The pitch is "your company's e-mail already knows who the customer is" — nobody
migrates for the CRM, they migrate for the e-mail and stay for the rest. A light CRM/Support is a
natural upsell; a heavy CRM is a reason not to buy either.

Names (two collisions to fix): `user_groups` → **Teams**; `contact_groups` → **Labels**.
Task-vs-ticket rule, written into the UI: *did it come from outside with a deadline? Ticket. Did
we decide it? Task.*

Assumed cost: part of the UI built on 07/15 (the visibility selector, `CompaniesView`,
`ContactPage` with tabs) gets rewritten. Better now, with 7 contacts and 1 company, than with real
customers in there.

## State (2026-07-15, end of day) — FINAL model

There were two turns today. Recording all three so the next person doesn't retrace the path:

1. **Morning**: `contacts` served both address book (private) and customer (shared) at once →
   contradiction (symptom: "Ticket #12 from —"). We cut the 3-level visibility selector and split
   into `contacts` (address book) + `clients`/`client_contacts` (CRM). Migration 00022.
2. **Afternoon**: the split fixed the contradiction but created **two places for "people."** The
   customer noticed and said: *"I don't like it, we're mixing concepts."* The original complaint
   was never the single table — it was the **ceremony** around privacy.
3. **Final** (migration 00023): **one single `contacts`**, `kind` = company|person, `company_id`
   points to the company-contact. Contacts goes back to the rail; Support lives inside it.

Rules (decided with the customer, tested):

| | Rule |
|---|---|
| **View** | by **GROUP**: I'm the owner, or I'm on a **team** that one of the contact's groups was shared with. A person inherits from their company. An unshared group = only the owner sees it. |
| **Edit/delete** | **owner only** (`UpdateContact` filters by `owner_user_id`; the UI shows "Read-only"). |
| **Notes** | whoever **can view** can comment — commenting isn't editing. |
| **List** | companies and people together; a person shows `title · company`. Filter All/Company/Person + search. |

Names, collision resolved: **Groups** = contact groups (grant access). **Teams** = `user_groups`.
"Label" was dropped: a label doesn't grant permission, a group does.

Verified running (Chrome, real sessions): Ana sees 8 contacts and her groups; **Bob sees 3** (only
the ones in the group shared with his team), without Ana's groups, and the page says
**"Read-only"** with no edit button. Backend: sharing/unsharing the group makes the contact
appear/disappear for Bob; Bob can't share Ana's group (404); PATCH on Ana's contact → 404; a note
→ 201.

A trap caught during the migration: I was about to create a "Customers" group for the former
customers, but Ana already had one with that name — without a unique constraint there would have
been two identically-named groups, and the `JOIN ... ON name` would have matched both, sharing her
**personal** group with the sales team. I created "Company contacts" instead; her "Customers" stays
hers alone.

Missing: phase **6b** (MIME sending with `Message-ID` + Stalwart inbound hook).

## Phases

### 1. Unify identity (foundation for everything)
Today there are two worlds: `panel_users` (the `/app` panel, RBAC, MFA, password) and `mail_users`
(webmail, mailbox on Stalwart). Creating a user creates **both** with the same e-mail and the same
role — and changing the role at `/members/{id}/role` updates **only the panel**, leaving
`mail_users.role` stale. That bug is why unification comes first.

- `mail_users` absorbs: `password_hash`, `language`, `totp_secret`, `mfa_totp_enabled`,
  `mfa_sms_enabled`, `phone`.
- Repoint FKs: `sessions.user_id`, `recovery_codes.user_id`, `invites.invited_by`,
  `mfa_challenges` → `mail_users`.
- Migration: match by (tenant, e-mail); a `panel_user` without a `mail_user` (a signup account,
  still without a mailbox) becomes a `mail_user` with a null `stalwart_account_id`.
- **Note:** `mail_users` now means "user," with or without a mailbox.

### 2. Admin in the webmail menu
- **One icon** in the rail → internal menu (same pattern as E-mail): Users, Domains, Billing,
  Groups.
- **RBAC enforced on both sides**: the item disappears from the UI **and** the API refuses it
  (real defense, not just UI).
- Existing permissions: `domains.view/manage`, `mailboxes.manage`, `members.view/manage`,
  `billing.manage`, `usage.view`, `tenant.delete`.

### 3. User groups
- `user_groups` + `user_group_members` (≠ `contact_groups`, which is the contact catalog).
- **Creating a group automatically creates a private chat channel** with the members.
- An admin adds users to groups when creating/editing a user; groups are managed in the admin
  area.

### 4. Rich contacts
- `companies` (company) + `contacts.company_id` + `contacts.title` (job title).
- Phone numbers with flags: **is WhatsApp**, **can call**, **can e-mail** (consent — LGPD).
- **Privacy**: `visibility` = `private` | `groups` | `tenant`; `contact_shares(contact_id, group_id)`.
  Every contact query now filters by owner + visibility + the user's groups.

### 5. CRM
- A modern detail page per customer (company and person).
- **Notes** (timeline), **tasks** (a Kanban card linked to the contact), an aggregated
  **history**.

### 6. Tickets — REMOVED (00024)

Built (00021) and removed the same day, at the customer's request. The reason is consistent with
the positioning: the product is "simpler and more streamlined than Workspace," and a helpdesk
with SLA/queue/thread/first-response is **a different product**. The contact ended up with
**notes** and **tasks**, which is what actually handles a small company's day-to-day.

The full design remains in `00021_tickets.sql` and in git history, should it come back one day.
Removed along with it: the SLA policy (Admin → SLA), "open a ticket from the e-mail" in the
reader, and the phase 6b e-mail integration (never built).

**Technical finding that survives the removal** (relevant to anything that needs inbound e-mail):
Stalwart 0.16.4 **accepts MTA hooks over JMAP** — `x:MtaHook/get` responds (empty list) in the
admin session with `urn:stalwart:jmap`. In other words, Stalwart **can call our API** when a
message arrives, without a service mailbox with a password stored in our database (which would
violate law §2.6) and without polling. `x:Setting/get` doesn't exist — configuration is
per-object (`x:MtaHook`, `x:Domain`, …).

What the backend does NOT have, and would need for real outbound e-mail: MIME with `Message-ID` /
`In-Reply-To` / `References`. The only sender is `notify/email.go` (`net/smtp`, plain text, 1
recipient, no attachment/HTML).
