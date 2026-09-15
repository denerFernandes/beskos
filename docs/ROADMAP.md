# Roadmap — Meet (rooms, controls, notes, transcription)

Current state and next steps for video calling. Cost decisions in
[§Transcription](#v2--ai-transcription-and-notes).

## v1 — now (in progress)

| Item | Description |
|---|---|
| **Room in its own window** | The room always opens in a separate window (`/room/:room`), with just the call — no app rail or top bar. The webmail stays usable behind it. |
| **Room access (inside the call)** | The organizer switches between **Open** (anyone can join) and **Approval** (lobby) without leaving the room. |
| **Organizer controls** | Toggle per participant: **microphone**, **camera**, **screen share**, **messages/reactions**. Actually enforced (token grants + LiveKit `UpdateParticipant`), not just hiding a button. |
| **Host = room owner** | Ad-hoc rooms (e.g. a call started from chat) now get a record: whoever starts it becomes organizer. |

## v1.5 — ✅ DONE (2026-07-16)

| Item | State |
|---|---|
| Meeting notes | ✅ collaborative panel in the room |
| Notes → tasks | ✅ a selection becomes a card (with AI) |
| Notes → e-mail | ✅ host button; sends to participants and **counts** (`{sent, failed}`) — a bare "sent!" that hides half the failed deliveries is worse than silence |
| Organizer joins first | ✅ `meetings.lock_until_host` (migration 00025). The refusal happens BEFORE signing the token: the guest gets `locked:true` and no token at all. The waiting room retries on its own every 5s |
| Remove participant | ✅ LiveKit's `RemoveParticipant`, owner only; the host can't remove themself (with the lock on, that would shut everyone out) |

**Recorded decision:** if LiveKit doesn't respond to the host presence check, we do **not** lock
anyone out — the room existing matters more than the lock, and the fault is ours, not the guest's.

**Missing prerequisite that got resolved:** the e-mail send would never have gone out —
`NOTIFY_SMTP_HOST` wasn't configured and `notify.Email` silently becomes `nil`. On top of that,
Stalwart's **port 587 doesn't work** (the listener closes the connection); only **465 with
implicit TLS** works, and the sender was doing `Dial`+`STARTTLS`. Fixed and verified: e-mails
reach a real mailbox.

## v1.5 — original (reference)

| Item | Description | Depends on |
|---|---|---|
| **Meeting notes** | A collaborative notes panel inside the room (free text, live between participants). | — |
| **Notes → tasks** | Select passages from the notes → becomes a Kanban card (reuses `POST /boards/{id}/cards` + the `/ai/task` AI). | Notes |
| **Notes → e-mail** | Send the meeting summary to the participants on close. | Notes |
| **"Organizer joins first"** | Locks the room until the host arrives (Meet has this). | — |
| **Remove participant** | LiveKit's `RemoveParticipant`, host-only. | — |

## v2 — AI transcription and notes

**Cost decision (Jul 2026):** transcribing is cheap — the bottleneck is privacy, not price.

| Option | Cost per meeting-hour | Note |
|---|---|---|
| **Groq** (whisper-large-v3-turbo) | **US$ 0.04** | 216× realtime. Recommended. |
| Groq, per-participant track | US$ 0.20 (5 people) | Perfect speaker attribution, no diarization needed. |
| AssemblyAI / Deepgram / OpenAI | US$ 0.15 – 0.36 | 4–9× more expensive for the same result. |
| Streaming (live captions) | US$ 0.44 – 1.02 | 2–10× the batch cost + a new service (LiveKit Agent). **Not needed** for notes. |
| **Self-hosted Whisper** | R$ 0 marginal | Data never leaves the VPS (LGPD). Uses CPU (1h ≈ 20–60 min) and worse pt-BR than `large`. |

**AI notes:** ~US$ 0.005 per meeting on DeepSeek (already configured). Negligible.

**Conclusion:** ~**US$ 0.045 per meeting-hour** (Groq + DeepSeek). A customer with 100 h/month
costs **US$ 4.50/month**. The real decision is **LGPD**: if customer audio can't leave Brazil,
swap the engine for local Whisper while keeping everything else the same (the STT interface stays
the same).

**Planned architecture:** post-meeting (batch), reusing the recording that Egress already uploads
to MinIO → extract audio → STT → transcript → DeepSeek generates notes/decisions/tasks. No
real-time infrastructure.

v2 scope:
- Recording transcript (pluggable engine: Groq | local Whisper).
- Automatic notes: summary, decisions, open items.
- Suggested tasks → Kanban (with user approval).
- Search inside transcripts.

## v3 — later

- Live captions (LiveKit Agent + streaming STT).
- Live translation.
- Breakout rooms.
- Polls and Q&A.
- Per-room virtual background (per-user today).
