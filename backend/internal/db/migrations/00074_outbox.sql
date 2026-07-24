-- +goose Up
-- Outbox de envio server-side: a mensagem é criada como rascunho no JMAP e o envio real fica agendado
-- aqui. O worker submete quando `send_at` vence (via EmailSubmission/set com a credencial brokerada).
-- Serve dois casos: undo-send (send_at = agora + janela curta; cancelar antes = não envia) e envio
-- agendado gerenciável (listar/cancelar). Sobrevive a fechar a aba — o cliente não precisa ficar vivo.
CREATE TABLE outbox_emails (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    email_id    TEXT NOT NULL,            -- id do rascunho no JMAP (Email)
    identity_id TEXT NOT NULL,            -- identity JMAP do "De:"
    from_email  TEXT NOT NULL,
    rcpts       TEXT[] NOT NULL,          -- destinatários do envelope (to+cc+bcc)
    subject     TEXT NOT NULL DEFAULT '', -- só para a UI de gestão
    send_at     TIMESTAMPTZ NOT NULL,
    status      TEXT NOT NULL DEFAULT 'pending', -- pending | sent | canceled | failed
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- O worker varre os pendentes vencidos; índice parcial mantém a varredura barata.
CREATE INDEX idx_outbox_due ON outbox_emails(send_at) WHERE status = 'pending';
CREATE INDEX idx_outbox_user ON outbox_emails(user_id, status);

-- +goose Down
DROP TABLE outbox_emails;
