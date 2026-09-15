# Plan — repositioning the landing page around European data sovereignty

> Status: **Phases 0 through 5 implemented and live** (see §7b to §7e). Written 2026-07-29.
> Still missing: `/public-sector` and language tier 2 (Phase 6), both dependent on traction.
> The app (`frontend/`) still ships in two languages — only the landing page has been translated
> so far.

## 0. Decisions already made

| Topic | Decision |
|---|---|
| URLs | **Always in English**, in every language: `/sub-processors`, `/sla`, `/dpa`, `/security`, `/privacy`, `/cookies`, `/terms`, `/acceptable-use`. Only the content is translated |
| Files | R2 moves to **EU jurisdiction** (instead of switching providers) |
| AI assistant | DeepSeek is no longer called directly: it now goes through **TensorX** (`tensorx.ai`) |
| Embeddings | OpenAI is out; **`qwen/qwen3-embedding-8b`** via TensorX is in |
| Languages | The eight proposed, **`pt-BR` excluded** for now |
| Language scope | Landing page **and app** — not just the site |
| Legal pages | The eight from the design: Privacy, Cookies, Terms, Security, DPA, Acceptable Use, SLA, Sub-processors |
| AI | **TensorX runs 100% in the EU** — AI is included in the "processed in the EU" sentence, no footnote |
| New sub-processor notice | **14 days** |
| Entity | **Open Solutions** (trade name of *Dener Santos Fernandes*, sole proprietor — JDG, not an sp. z o.o.) · NIP **PL7773457857** · REGON 543340675 · Poznań |
| Certifications | **None yet** — the security page describes practices, doesn't claim standards |
| Registered address and objections | **Wawrzyńca Engeströma 10, 60-571 Poznań, Poland** · NIP PL7773457857 · REGON 543340675 |
| Status page | **Yes**, at `status.beskos.com`, on the same infrastructure for now (a separate VPS is coming) |
| Tone | **Corporate**, the same in every language — the formal register in each one |
| Insurance | **Not for now** — which requires limiting liability in the Terms (see §2b) |
| SLA | **Out for now.** Instead, a *service commitments* page with no credits (see §4c) |

## 1. What changes

Today the landing page sells **stack replacement by price**: *"Cancel four subscriptions. Pay
one."* It works, but it's the easiest argument to copy — any cheaper suite says the same thing,
and it attracts people who buy on price and leave on price.

The new axis is **sovereignty**: where the data sits, who controls it, and who the company
hosting it answers to. Price becomes the second argument (good for closing, bad for opening).

| | Before | After |
|---|---|---|
| Promise | Nine tools, one account, cheaper | The whole team's work under European jurisdiction |
| Proof | Savings calculator | Where the data runs, who operates it, which law applies |
| Comparison | Google/Microsoft on price | Google/Microsoft on **legal exposure**; Proton/Nextcloud on **scope** |
| Objection to overcome | "it's too cheap to be good" | "what if I need to leave?" / "who are these people?" |
| Who buys | Cost-conscious SMBs | Whoever has to justify the choice to a DPO, an auditor, or a tender |

What sets us apart from our neighbors: **Proton** is individual privacy growing into teams;
**Nextcloud/openDesk** is software someone has to host and maintain; **Infomaniak** is the
closest peer (Swiss, not EU). We are a **complete, managed, EU** suite — email, Office, drive,
meetings, chat, calendar, wiki, boards, and an assistant, on one account and one invoice.

---

## 2. The blocker: the stack isn't yet what the message will say

This comes first because **the page cannot be published before this is resolved**. Saying "100%
in the EU" with today's stack would be false, and it's exactly the kind of claim a public-sector
buyer checks.

| Piece | Today | Problem for the message | Fix |
|---|---|---|---|
| Servers (app, DB, mail) | **Netcup**, Germany | None — it's the best argument we have | Use: "servers in Germany" |
| Files (Drive) | **Cloudflare R2**, jurisdiction moving to **EU** | Data stays in the EU; Cloudflare itself remains American (the CLOUD Act applies to the company, not to where the bytes sit) | Decided: keep it, with EU jurisdiction. The page must say it this way, without promising "beyond US reach" |
| Email sending | **Amazon SES** (eu-west-1) | Data in Ireland, but an **American** provider | European relay, or send directly from Stalwart |
| DNS/CDN/TLS | **Cloudflare** | All traffic passes through an American edge | Bunny (SI), or Cloudflare as DNS only, with TLS at the origin |
| AI assistant | DeepSeek **via TensorX (100% EU)** | Resolved: content no longer leaves the EU | Write on the page: model served in the EU, not by the model's creator |
| Wiki embeddings | **`qwen/qwen3-embedding-8b`** via TensorX (EU) | Resolved | Same |

**With the decisions made, this remains:** sending (SES) and the CDN (Cloudflare) stay
American, and R2 remains American with data in the EU. In other words, the sentence the page can
support is **"your data is processed and stored in the EU"** — not "none of our providers are
American," which would be false.

The difference isn't rhetorical: it's the first question a German DPO asks. The honest way out,
and the one that sets us apart, is for the **sub-processors page** to say everything — who, for
what, where, with what safeguard. Whoever publishes the list wins the conversation even with
American providers in it; whoever hides it loses it the moment someone asks.

**[decide]** does swapping SES for a European relay and the CDN for Bunny wait until after
launch? I recommend yes, but with a set date — and the sub-processors list shows the progress on
its own.

---

## 2b. The legal entity (and what it requires)

I checked the NIP in VIES: **PL7773457857 is valid for intra-community operations**, which
confirms the reverse-charge mechanism we've already implemented works with us as the seller. Two
things stood out:

1. **Address:** we publish `Wawrzyńca Engeströma 10, 60-571 Poznań` — a virtual office. VIES
   returns the **residential address** (`Skrzynki, Łagodna 7 m1`), which is what's on record.
   Publishing the virtual address is correct (you don't put anyone's home on a public page), but
   there's a step to take: **register Poznań with CEIDG as the correspondence address**
   (*adres do doręczeń*) and confirm the virtual office contract accepts receiving legal
   notices. That way what's published, what's on record, and what's on the invoices all line
   up — without it, an official letter goes to your home while the customer trusting the page
   is looking at a different address.
2. **The legal subject is the person, not the brand.** Under a JDG, the contracting party is
   *Dener Santos Fernandes*; "Open Solutions" is a trade name. The legal pages must identify it
   this way: *"Open Solutions — Dener Santos Fernandes, Poznań, NIP PL7773457857, REGON
   543340675."* Hiding the name isn't an option: provider identification is mandatory
   (Article 5 of Directive 2000/31/EC and Polish electronic services law), and in a market that
   buys trust, being clear works in our favor.

**What this costs, stated plainly:** as a sole proprietor, you're liable with your personal
assets. An SLA with credits and a DPA with indemnities are real obligations. And there are
public tenders and large companies that require a limited company — a JDG is enough for SMBs, it
stops at large contracts. This isn't a blocker today; it's a marker for when the first serious
customer shows up.

**With no insurance (decided), protection becomes contractual.** That's legitimate, but it has
to be written down:
- **liability limitation** in the Terms — a cap equal to what the customer paid in the last 12
  months, which is the industry standard;
- **exclusion of indirect damages** (lost profits, third-party data loss);
- **SLA credits as the sole remedy** for downtime, paid as account credit, not cash.

Without these three clauses, an SLA with credits and a DPA with indemnities point straight at
personal assets. With them, the risk is scoped to the contract. The sp. z o.o. and insurance
wait for the first contract that requires them.

**One page is missing** from the eight: `/imprint` (legal identification). It's expected in
Germany and Austria, and it resolves the obligation in the rest. Cheap to make: name, address,
NIP, contact, the person responsible.

---

## 3. Message

**Promise (headline):** your team's work — email, files, documents, meetings — under European
law, operated by a European company.

**The three proofs** (the page has to show them, not just state them):
1. **Where** — named servers, country, operator. A simple map, no marketing gloss.
2. **Who** — a Polish company, no American holding, a public sub-processors list.
3. **What** — the scope: mail, Office, drive, meetings, chat, calendar, wiki, boards, AI.

**Objections to answer on the page itself** (each deserves a section or an FAQ entry):
- "What if I want to leave?" → export in open formats, no lock-in contract.
- "Can it survive my audit?" → DPA, sub-processors, retention, encryption, access logs.
- "What if it breaks?" → replication, backups, uptime history.
- "It's a small company." → yes; and the mail server's code is open, the data is exportable,
  and there's no lock-in.
- "Doesn't AI break all of this?" → where the model runs, what leaves the EU, how it's turned
  off.

**Tone: corporate, and the same across every language** (decided). This changes the current
copy, which is informal and imperative (*"Cancel four subscriptions"*). In practice, per
language:

| Language | Formal address |
|---|---|
| Portuguese (PT) | Third person — formal verb forms, e.g. referring to "your team" and "may export" |
| English | Neutral-formal; no slang, no exclamation marks |
| German | **Sie** (never *du*) |
| French | **vous** |
| Spanish | **usted** |
| Italian | **Lei** |
| Dutch | **u** |
| Polish | **Pan/Pani/Państwo** |

Corporate here means **sober and precise**, not pompous: short sentences, concrete numbers, zero
superlatives. It's also the register a sovereignty buyer expects: whoever decides where a
company's data lives doesn't want a site talking to them like a friend. This goes into the
translation prompt, so it's cheap to apply and to fix.

**What NOT to say:** "guaranteed compliance," "ISO certified" (when it isn't), "immune to the
CLOUD Act" (no one is immune — what exists is not being subject to it), "your data never leaves
the EU" while R2/SES/DeepSeek are still there.

---

## 4. Site architecture

**URL rule:** always in **English**, the same across every language (`/de/sub-processors`,
`/fr/sub-processors`). Content is translated, not the path — links shared between colleagues in
different countries keep working, and legal documents are cited by the same address in any
contract.

**New — product and argument**

| URL | Why |
|---|---|
| `/sovereignty` | The central argument. Where the data sits, which laws, which providers, what changes versus the US |
| `/migration` | The biggest purchase blocker: leaving Google/Microsoft without losing mail or the calendar |
| `/public-sector` | Public procurement, hosting requirements, invoicing, references |
| `/product/<app>` | One per app (mail, drive, docs, meet, chat, calendar, wiki, boards): this is what brings search traffic |
| `/compare/opendesk`, `/compare/proton`, `/compare/infomaniak` | We already have Google/Microsoft; missing the ones against European neighbors |

**New — legal and trust** (the eight from the design). These are the backbone of the message:
what sets us apart isn't saying "sovereignty," it's having this written down and public.

| URL | What it covers |
|---|---|
| `/privacy` | Privacy policy (exists, to be revised for the new positioning) |
| `/cookies` | Cookie policy — separate from privacy, as in the design |
| `/terms` | Terms of service (exists) |
| `/security` | How we protect data: encryption in transit and at rest, access, backups, replication, incident response, responsible disclosure |
| `/dpa` | Data processing agreement, **full text on the page** (the TensorX template is good): definitions, roles, data breaches, sub-processors, transfers, audit + Annex I (processing details) and Annex II (technical measures and SCCs) |
| `/acceptable-use` | What can't be done on the platform — this is what lets us suspend abuse without argument |
| `/sla` | **Service commitments**, not a contractual SLA (§4c): a 99.5%/month target, maintenance windows, support response targets, and the sentence saying it isn't a guarantee |
| `/sub-processors` | The full list: **Sub-processor · Purpose · Location · Transfer mechanism**, with a last-updated date, a commitment to **prior notice** before adding a new one, the customer's right to object, and a notice subscription |

The sub-processors page is the most important of the eight. It's where you see that Cloudflare
and AWS exist in the stack — and it's precisely by saying so that the conversation is won. With
the current decisions, the list holds: Netcup (servers, DE), Cloudflare R2 (files, EU
jurisdiction), Cloudflare (DNS/CDN), Amazon SES (sending, IE), TensorX (AI), Stripe (payments),
and whatever else gets added.

Prior notice set at **14 days**. Objections go to the registered address (Poznań) and — because
no one mails a letter to object to a sub-processor — also to **`privacy@beskos.com`**, which is
the practical channel. The status page now exists (§4b) and is the source of the SLA's
measurement.

**Changing:** the homepage (new hero and new order — sovereignty, scope, proof, price),
`/compare` (adding a "jurisdiction" column), `/pricing` (keeping the calculator, which is good,
but placed further down).

---

## 4c. No SLA (for now)

Decided: **there's no SLA**. It's the right call for a one-person operation — an SLA with
credits is a debt that comes due at three in the morning, and with no insurance and no limited
company, it points straight at personal assets.

But publishing nothing about availability has a cost too: it's the second question any buyer
asks. The way out is a **service commitments** page — same `/sla` URL, different content:

| Instead of… | We write |
|---|---|
| "99.5% guaranteed, with credits" | "We work toward **99.5%** per month. This is not a contractual guarantee" |
| Automatic credits | "If we fail for a prolonged period, we'll talk to you" |
| Downtime compensation | Announced maintenance windows, public history on the status page |
| — | Support response times as a **target**, not a promise |

This gives the buyer what they need (knowing what to expect) without creating an obligation we
can't currently honor. And it's consistent with the rest of the message: whoever publishes the
sub-processors list is also the one who admits to having no SLA.

**When it will make sense again:** with a limited company (sp. z o.o.), insurance, and someone
on call. Then the SLA becomes a selling point instead of a risk.

**In the Terms, this has to be explicit:** service provided on a *best-efforts* basis, a
liability cap at 12 months paid, exclusion of indirect damages. Without an SLA, these clauses
matter even more — they're the only boundary that exists.

---

## 4b. Status page

Decided: it exists, it's public, and it covers every API. It's what backs the SLA — without a
measurement the customer can see, "99.5%" is just our claim against their memory.

**What gets measured** (one component per row on the page):

| Component | Probe |
|---|---|
| App (app.beskos.com) | GET the page, status code 200 |
| API | public health endpoint, with a database check |
| Mail — delivery (SMTP) | connect to `mail.beskos.com:465` + `EHLO` |
| Mail — reading (IMAP/JMAP) | JMAP session with a test account |
| Drive / files | write and read a small object in storage |
| Documents (OnlyOffice) | editing service health |
| Meetings (LiveKit) | room server health |
| Assistant (TensorX) | minimal request to the model |
| Operator console | GET the page |
| Site | GET the page |

**Where it lives:** `status.beskos.com`, on the same infrastructure for now, with a move to a
separate VPS on the way. **No notice on the page** about that limitation (decided) — and the
risk is small: during a total outage the page becomes unreachable, which is much better than a
page claiming everything's fine when it isn't. During partial outages, which are most of them,
it works and is useful.

The subdomain is convention (`status.stripe.com`, `status.github.com`) and what people type by
hand when something breaks. It also saves the migration: when the page moves servers, only a DNS
record changes and nothing else. `/status` on the site redirects there.

**How:** [Uptime Kuma](https://github.com/louislam/uptime-kuma) (open source, self-hosted,
hostable in the EU) covers 90% of it — probes, history, a public page, notices, RSS.
Alternative: write our own, which only makes sense if we want deep probes (send an end-to-end
test email and measure how long it takes to arrive). I recommend starting with Kuma and adding
the deep probes only afterward.

**What the page must show:** each component's status, **90 days** of history, incidents with a
timeline (detected → cause → resolved), and email subscription. The same subscription serves the
sub-processor notices — it's a single list.

**[decide]** `status.beskos.com` (subdomain) or `/status`? I recommend the subdomain: if the
main ingress goes down, `/status` goes down with it.

---

## 5. Languages

Today: `pt` (neutral, leaning Brazilian), `en`, `es`. The structure is already in place
(`/[locale]`, typed dictionaries) — adding languages is translation work, not code work.

**Tier 1 — at launch**

| Language | Why |
|---|---|
| **English** | The EU's business lingua franca; serves Ireland, the Nordics, the Baltics, and anyone without their own language |
| **German** | The biggest market, the biggest sovereignty budget, and the one that values "servers in Germany" most |
| **French** | An established sovereignty policy; an audience used to national alternatives |
| **Polish** | Our home base: local credibility, local invoicing, local tenders |
| **Spanish (ES)** | A large market, growing SaaS adoption |
| **Italian** | Same, plus an active public sector on national cloud |
| **Portuguese (PT)** | You asked for it; a small but home market |
| **Dutch** | The Netherlands + Flanders: among the EU's highest SaaS spend per person, and a live political debate over US dependency |

**Tier 2 — after validating traction**: Swedish, Danish, Czech, Romanian.
Swedish and Danish because the municipal debates over Microsoft 365's legality are exactly our
conversation. Czech and Romanian for size and weak competition.

**Leaving out for now:** Norwegian (outside the EU — the story changes, and they read English),
Finnish, Greek, Hungarian, and the Baltics (small and comfortable in English). Malta and
Ireland: English.

**Decided:** `pt-BR` is out for now. Portuguese becomes **European Portuguese** — which requires
a vocabulary pass over what already exists (Brazilian → European terms throughout), on both the
landing page and the app.

**English becomes the source of truth.** Today it's `pt.ts`. With eight languages, translating
from English is cheaper, more reliable, and what any agency or tool expects.

### And the app, which is the big job

The languages aren't just for the site. The app currently has **2,413 strings per language**
(110 KB of JSON), in `pt-BR` and `en`. Going to eight multiplies that by four relative to what
exists today:

| Surface | Where | Status | Work |
|---|---|---|---|
| App (webmail, admin panel, sign-up, onboarding) | `frontend/src/i18n/locales/<lang>/common.json` | 2,413 strings × 2 languages | +6 languages, and converting `pt-BR` to `pt-PT` |
| Server errors | same files (`server.<code>`) | already translated by the client | come along with the rest |
| **Transactional emails** | `backend/internal/notify/messages.go` | **hardcoded in code**, pt and en only | pull them into files and translate — this is what the customer reads outside the app |
| Landing page | `landing/lib/locales/…` | pt, en, es | +5, plus the pt-PT conversion |
| Operator console (manager) | `manager/src/lib/strings.ts` | internal | **do not translate** — this is for us |

Two things to settle now, because they affect the design:
- **Per-user language** already exists (`mail_users.language`) and so does the selector. Still
  to decide: should the organization set a default language for new users? **[decide]**
- **Emails go out in the recipient's language** — already the case; but with eight languages the
  text should live in translation files like the rest, not in Go.

**Realistic cost per language:** ~700 lines of text today, ~1,200–1,500 with the new pages. Per
language: translation + native review + ongoing maintenance of every future page. Maintenance is
what bites — every new section becomes eight. I suggest locking the content in
Portuguese/English, freezing it, and only then translating everything at once.

---

## 6. Mechanics (SEO and i18n)

- **English URLs in every language.** Some local SEO is lost (a `/de/preise` page would rank
  better in German than `/de/pricing`), but it's gained back in sharing, support, and legal
  citation. It's offset by the `<title>`, `<h1>`, and content being in the local language, which
  matters most anyway.
- Reciprocal `hreflang` across the eight, plus `x-default` for English.
- `Accept-Language` detection already exists; keep the selector visible — visitors from abroad
  don't want to be guessed at.
- Currency by country already exists (5 currencies) — add zloty if it's missing, and show prices
  **with VAT** for consumers and **without** for businesses, with the reverse-charge note.
- **Legal pages must be dated and versioned.** A visible "Last updated," and history kept: a
  customer who signed up in March has the right to see March's text.
- Terms, privacy, and the DPA are **legal** text: legal review per language, not machine
  translation.
- Country pages (`/de/…` featuring "servers in Germany") are worth more than generic
  language pages.
- In the app, the same dictionary serves eight languages: worth a CI check that **no key is
  missing** in any language (TypeScript already guarantees this on the landing page, but not in
  the app's JSON).

---

## 6b. Translation: all by AI, with manual review afterward

**Decided:** we translate everything by AI and Dener reviews it. No agency, no freelancers.

Real volume (counted, in English): **app ~8,400 words** (2,413 strings) + **landing page
~3,200 words** (~500 strings) ≈ **11,600 per language today**, and about **25,000** with the new
pages. Six new languages.

### What this requires to go well

**1. Glossary first.** Before the first word: how *mailbox, drive, board, meet, workspace,
sub-processor, seat, tenant* are said in each language. Without a glossary, the same thing gets
two names on different screens — the most visible flaw of machine translation, and the most
expensive to undo afterward.

**2. Plurals aren't all the same.** This is the technical error that shows up most:

| Language | Forms i18next needs |
|---|---|
| en, de, es, it, nl, sv | `_one`, `_other` |
| fr | `_one`, `_many`, `_other` |
| **pl** | `_one`, `_few`, `_many`, `_other` |
| pt | `_one`, `_other` (but the word for "millions" pulls `_many` in some cases) |

Today we have keys like `unreadElsewhere_one/_other`. Polish needs **four** — if the translation
doesn't generate them, the app shows the raw key to a Polish customer. The script has to know
this per language, and the missing-keys test has to check for it.

**3. Placeholders and formatting must stay intact.** `{{count}}`, `{{email}}`, `<b>`, `\n` — AI
sometimes translates the placeholder name or eats the punctuation. Automated validation: every
translated string must have exactly the same placeholders as the original.

**4. German runs ~30% longer than English.** Buttons, menus, and the app rail will overflow.
After translating: walk through the main screen, settings, and sign-up in German and see what
overflows. This is layout work, not translation work.

**5. Legal: English prevails.** With AI translation this stops being advice and becomes a
condition. Every translated legal page carries: *"This translation is provided for
convenience. In case of discrepancy, the English version prevails."* One legal review **in
English** (a Polish lawyer with GDPR/SaaS practice), and the translations follow by AI with no
contractual risk.

**6. The limit of manual review, stated plainly.** You review Portuguese, English, and Spanish
competently, and Polish to some extent. **German, French, Dutch, and Italian have no
reviewer** — and those are three of the markets we want. Two cheap safety nets:
- a discreet *"report a translation error"* link in the footer, opening an email with the page
  and the language;
- treating the first customers in each market as reviewers: a friendly request catches more
  errors than an agency does.

### How the script will work (once we start)

1. Reads the **English** dictionary (source of truth) and the glossary.
2. Translates in batches, with the key's context (`webmail.pwHintShort` says more than the
   sentence alone).
3. Generates the correct plural forms for the language.
4. Validates: matching placeholders, no missing keys, valid JSON, complete plurals.
5. Writes the language file and shows a `diff` for review.
6. Runs again on only what changed — doesn't retranslate what's already been reviewed (a
   per-key review mark).

---

## 7. Phases

| Phase | What | Depends on |
|---|---|---|
| **0** ✅ | Stack: R2 to EU jurisdiction, AI and embeddings via TensorX (`qwen3-embedding-8b`) | **Done** 2026-07-29 — see §7b |
| **1** | **Status page** (on current infrastructure) + the nine legal pages in English and Portuguese, with English URLs. Start with sub-processors and the DPA | Phase 0, so the list reflects reality · `/sub-processors` ✅ |
| **2** ✅ | New message: homepage, `/sovereignty`, `/security` | **Done** 2026-07-29 — see §7c |
| **3** | `/migration`, per-app pages · comparisons with Proton/openDesk/Infomaniak ✅ | Phase 2 |
| **4** ✅ | German, French, Polish — **landing page done**, app still to do | Stable content |
| **5** ✅ | Italian, Dutch; `pt-BR` → `pt-PT` on the landing page | Phase 4 |
| **6** | `/public-sector` + tier 2 (Swedish, Danish, Czech, Romanian) | Traction |

There's a reason for the order: translating before content settles means paying for the
translation twice. That's why the languages come after the pages, not at the same time.

---

## 7b. Phase 0 — what got done (2026-07-29)

**Files.** Jurisdiction doesn't change inside an already-created R2 bucket: we moved to
**`beskos-data`**, created in EU jurisdiction (endpoint `<account>.eu.r2.cloudflarestorage.com`).
The 73 objects in the old bucket were copied and **verified one by one** by size and md5 — a
cross-jurisdiction copy has to go through the client, there's no `CopyObject` from one to the
other. A second pass after the cutover showed no differences. The old bucket **stays intact**, as
a safety net; deleting it is a later decision, and nothing gets deleted mid-flight.

**AI.** `AI_PROVIDER=tensorx`, same protocol (OpenAI-compatible), destination
`api.tensorx.ai/v1`. Model `deepseek/deepseek-v4-flash`. What this changes for the message: the
models are served on European infrastructure and the content **never reaches the people who
built them** — which is the sentence the sub-processors page now backs up.

**Embeddings.** `qwen/qwen3-embedding-8b`. Two surprises, both resolved on our end:

1. TensorX **refuses the `dimensions` parameter** with a 400 and always returns 4096
   dimensions.
2. The column is `vector(1536)` and pgvector's ivfflat index **doesn't go past 2000** — at 4096
   the index would be lost and semantic search would scan the whole table.

We cut the first 1536 components and renormalize (Qwen3 is trained with Matryoshka, just like
OpenAI's `text-embedding-3`: the meaning sits at the head of the vector). The renormalization
isn't a detail — search compares by dot product, and without it long texts would systematically
rank ahead. Migration `00127` clears `wiki_chunks`, because vectors from different models aren't
comparable; the worker reindexes on its own.

**One lesson worth recording:** the first Phase 0 commit described, in its message, a
`case "tensorx"` that never made it into `config.go`. In production it failed silently — AI off,
`/api/ai/briefing` returning 503 — and only surfaced by reading the startup logs. A commit
message isn't proof; a production log is.

---

## 7c. Phase 2 — what got done (2026-07-29)

**The homepage stopped selling price.** The hero became *"Your team's work, under European
law"* and the calculator moved down next to the pricing, with the framing flipped — *hosting in
Europe isn't the expensive option* instead of *save money*. New order: hero → where the data
sits → who operates it → scope → integration → AI → exit → cost → pricing → close.

**The proofs moved to the first fold.** Two new sections: `where` (a table of eight components
with operator, country, and transfer mechanism, generated from `lib/infrastructure.ts`) and
`who` (entity, address, and NIP/REGON, read from `lib/subprocessors.ts` so there's no double
source of truth). The three American providers appear there, with the SCCs declared. The legal
pages already existed since Phase 1 and the homepage didn't link to any of them; now each card
links to the one that proves it.

**`/sovereignty`** carries the full argument, with a *what we don't claim* section: no
certifications, no contractual SLA, three providers subject to foreign law, and migration
handled with assistance rather than automated.

**Comparisons on two axes.** The table gained `euJurisdiction` (which law the company answers
to) and `managed` (a managed service versus software you host) — without them, the comparison
played entirely on the incumbents' home turf. Proton, Infomaniak kSuite, and openDesk were
added: against these we don't win on jurisdiction, we win on scope, and the pages say so.
openDesk has no list price and the page omits the cost instead of inventing one.

**Languages.** English became the source of truth (`Messages = typeof en`) and the default
language; Portuguese became European and corporate register across the whole landing page, not
just the legal pages.

**Prices verified** (July 2026): Proton Business Suite USD 12.99/user/month billed annually;
Infomaniak kSuite Business CHF 7.33 (~€8.03), converted at the fixed exchange rate already
declared.

---

## 8. Everything is decided

| # | Decision |
|---|---|
| Address | Poznań published; CEIDG update in progress (Dener is handling it) |
| Status | `status.beskos.com`, same infrastructure, no limitation notice |
| SLA | Out; a commitments page stands in for it |
| Tone | Corporate, formal in every language |
| Translation | All by AI, manual review afterward |
| Languages | en, de, fr, pl, es, it, pt-PT, nl (tier 1) · sv, da, cs, ro (tier 2) |
| URLs | Always in English |
| Stack | R2 in EU jurisdiction; AI and embeddings via TensorX (100% EU) |

**Phase 0 done and live** (§7b), and the sub-processors page published in the current three
languages. Next, the rest of the legal pages and the status page.

---

## 9. A note on size

You said "a developer with a dream." It's worth deciding what to do with that, because the
message changes depending on it:

- **Hidden**, it's a ticking time bomb: whoever finds out late feels misled, and they always
  find out.
- **Stated**, it's an argument — in sovereignty, "one person in Poznań who picks up the phone"
  beats an American mega-company for a lot of SMBs, agencies, and freelancers. It's the same
  reason people choose the accountant down the street.

What it **doesn't** win is the public sector and large enterprise: there they ask for a limited
company, insurance, an SLA, and certification — which is why `/public-sector` should sit at the
end of the plan, not the start. The realistic target for these first few months is European SMBs
with 5 to 50 people who already distrust Google and Microsoft.

---

## 7d. Infrastructure table audit (2026-07-29)

The homepage now states, component by component, where each piece runs. These claims were
verified against production, not against documentation:

| Claim on the page | Verified as |
|---|---|
| App, database, and mailboxes in Germany | `mail.beskos.com` → 185.232.71.70 · RIPE: `NETCUP-NETWORK`, `country: DE`, netcup GmbH |
| Meetings and documents on our own servers | `meet.beskos.com` → 185.232.69.0 and `docs.beskos.com` → 185.232.71.70, both netcup |
| Files in EU jurisdiction | CapRover (api/worker): `MINIO_ENDPOINT=…**eu**.r2.cloudflarestorage.com`, `MINIO_BUCKET=beskos-data` |
| Sending via SES in Ireland | `SES_REGION=eu-west-1` |
| AI and embeddings in the EU | `AI_PROVIDER=tensorx`, `EMBEDDINGS_BASE_URL=https://api.tensorx.ai/v1` |

**One discrepancy found and fixed.** `deploy/secrets.env` still had the pre-Phase-0 R2 values —
the `beskos` bucket and the endpoint without `.eu.`. Production was correct (Phase 0 changed
CapRover), but the reference file wasn't kept in sync: anyone restoring the environment from it
would have sent files back to the bucket outside EU jurisdiction, silently invalidating the
claim the homepage now makes on the first fold. File aligned and commented.

**One point still to confirm.** RIPE gives `country: DE` for the block and netcup GmbH is
German, but netcup has data centers in Nuremberg, Vienna, Amsterdam, and outside the EU — the
city can't be verified from outside. The pages say "Nuremberg"; if the VPS were ordered
elsewhere, this is where it gets corrected. The "Germany" claim is verified; the city depends on
what the control panel shows.

**Ownership note:** netcup GmbH belongs to the Anexia group (Austria) — the network block shows
up as *powered by ANX*. Both countries are in the European Union, so the message doesn't change;
it's recorded here because it's the kind of detail an attentive buyer checks.


---

## 7e. Phases 4 and 5 — the landing page's eight languages (2026-07-29)

`en, de, fr, pl, es, it, nl, pt` are live. English is the source of truth and the default
language; Portuguese is now European Portuguese. **The app hasn't been translated** — it stays
in `pt-BR` and `en`, and that's the big job still ahead (2,413 strings, §5).

**Translated by script, not by hand** (`landing/scripts/translate.py`), as §6b anticipated.
That's ~1,600 lines of text per language, and the copy is still going to change: done by hand,
the first change to the English text would start the site diverging from itself. The script is
resumable — a run of 35 calls interrupted halfway through is the normal case.

**Four validations, and the order they appeared in says something:**

| # | Validates | Why it exists |
|---|---|---|
| 1 | Shape | `Messages = typeof en` — a missing key in Dutch fails to compile |
| 2 | Placeholders | a translated `{price}` shows up raw to the customer |
| 3 | Paths | an `href: "/dpa"` turned into `/vertrag` gives a 404, and is still a valid string |
| 4 | **Language** | `nl/home.ts` came out entirely **in German** and passed the previous three |

The fourth was written after the error happened. The file had the right structure, the right
placeholders, and the right paths, and it was only caught because `/nl`'s `<title>` showed up in
German during a routine check. The lesson worth keeping: **correct structure and the wrong
language passes everything that validates structure.**

**Per-language metadata.** URLs are in English by decision (§4); what makes up for that is the
`<title>` and the content being in the reader's language. They come from the route's dictionary,
reusing the hero and headers already translated.

**English prevails.** Every translated legal page opens with a notice that the English version
prevails. With machine translation this stops being a courtesy and becomes the contractual
boundary: one legal review in English covers all eight pages, and the translations follow with
no added risk.

**No native reviewer:** German, French, Italian, and Dutch. Every generated file says so in its
header, not in a note nobody reads.


---

## 7f. The public claim becomes "Europe" (2026-07-29)

Decided: the site stops saying **Germany** and starts saying **Europe** / **European Union**.
This changes the hero, the infrastructure table, the security page, the sub-processors list,
the pricing FAQ, and §7 of the DPA, across all eight languages.

**What we lose, stated in full:** "servers in Germany" was the site's most concrete and most
verifiable claim, and it's exactly what the German market — the plan's biggest target — values
most. A buyer comparing providers reads "Germany" as a fact and "Europe" as a category.

**What we gain:** the claim no longer depends on one machine's location. A VPS that moves data
centers within the EU no longer forces a rewrite across eight languages, nor creates a gap
between what the page says and what the infrastructure does — which is the risk §7d had already
caught once, in `secrets.env`. It's also the boundary GDPR itself uses: what's assessed is EU
versus third country.

The §7d verification still stands and hasn't been deleted: the servers are still netcup, with
`country: DE` in RIPE. What changed is the level of detail we publish, not the fact itself.


---

## 7g. File encryption changes the weakest line (2026-07-30)

The R2 line in the infrastructure table was the site's weakest: customer content on an American
company's servers, with the key on their side. Not anymore — files are now encrypted by us, and
the provider only ever holds ciphertext, as detailed in the design doc (filename kept unchanged) `docs/cifra-ficheiros.md` lang:ok

This changes the security page, Annex II of the DPA, the infrastructure table's note, and the
"we don't claim that none of our providers is subject to foreign law" item on `/sovereignty`.
The comparison with Proton gets **more** precise, not stronger: "files are encrypted with keys
we hold, which protects them from our storage provider — not from us."

**What couldn't be claimed, and is written down:**

- **Mail isn't covered.** Stalwart writes message content straight to the bucket and reads it
  from there directly; it doesn't go through our layer and we don't modify it (CLAUDE.md §5.4).
  The security page says this in our own words, and a limitation that already caused an outage
  doesn't get left out — it was discovered by accidentally encrypting it, documented at §2f of the design doc `docs/cifra-ficheiros.md` lang:ok
- **It isn't end-to-end encryption.** We hold the keys, because the product generates
  thumbnails, edits Office documents in the browser, and serves public links. Saying otherwise
  would be exactly the mistake `/sovereignty` exists to avoid.
- **Object storage has no second copy of ours.** Backups cover the databases; Drive files and
  recordings depend on the provider's durability. This is now written down, instead of the word
  "backups" covering everything. (Mail's copy **is** complete — verified on 2026-07-30 by
  restoring it and finding a message's text inside.)
