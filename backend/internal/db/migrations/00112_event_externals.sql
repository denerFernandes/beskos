-- +goose Up
-- Convidados EXTERNOS de eventos (23/07/2026): pessoas fora da organização, por e-mail.
-- Recebem o convite por e-mail (best-effort via notify) — não têm conta nem RSVP interno.
CREATE TABLE event_externals (
    event_id   UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    email      TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (event_id, email)
);

-- +goose Down
DROP TABLE IF EXISTS event_externals;
