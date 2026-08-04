-- +goose Up
-- The waiting list: people who asked to be told when there is room for them.
--
-- WHY A LIST AND NOT AN OPEN DOOR. The applications are not published yet and onboarding is done by
-- hand, one customer at a time. A signup form that lets anybody in produces accounts nobody
-- welcomed; a list produces a queue somebody decides on. It also collects the one thing that is
-- worth more than a signup at this stage: an address, a domain and a reason, from a person who came
-- looking.

CREATE TABLE waitlist (
    id      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Lowercased before insert. Asking twice is not two people, and the unique index below turns a
    -- second submission into an update rather than a duplicate somebody has to merge by hand later.
    email   TEXT NOT NULL,
    name    TEXT NOT NULL DEFAULT '',
    company TEXT NOT NULL DEFAULT '',
    -- Free text: what they want it for. The single most useful column on this table and the only
    -- one that tells us which segment is actually arriving.
    note    TEXT NOT NULL DEFAULT '',
    -- Their language, from the page they were reading. The invitation goes out in it -- writing to
    -- a German company in English is the first impression we would rather not make.
    locale  TEXT NOT NULL DEFAULT '',
    -- Which page sent them ("download", "pricing", …). Tells us what the person was looking at when
    -- they decided, which is the difference between guessing at demand and reading it.
    source  TEXT NOT NULL DEFAULT '',

    -- pending → invited → joined. 'joined' is set by hand for now: nothing links a signup back to a
    -- row here, and pretending otherwise would put a number on a screen that nobody computed.
    status TEXT NOT NULL DEFAULT 'pending',
    -- Carried in the invitation link so a click can be told from a coincidence. It does NOT gate
    -- signup today -- deciding whether the front door closes is a separate decision, and building
    -- the lock before it is taken would leave a lock nobody asked for.
    invite_token TEXT NOT NULL DEFAULT '',
    invited_at   TIMESTAMPTZ,
    -- Which operator sent it. An invitation is a promise made on the company's behalf, and it is
    -- worth knowing who made it.
    invited_by TEXT NOT NULL DEFAULT '',

    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One row per address. A second submission updates what they told us instead of creating a
-- duplicate for somebody to reconcile.
CREATE UNIQUE INDEX idx_waitlist_email ON waitlist (lower(email));
CREATE INDEX idx_waitlist_queue ON waitlist (status, created_at);

-- +goose Down
DROP TABLE IF EXISTS waitlist;
