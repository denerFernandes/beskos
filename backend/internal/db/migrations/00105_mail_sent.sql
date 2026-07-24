-- +goose Up
-- Medição de envio de e-mail por tenant/dia (relay SES + enforcement do plano, docs/manager.md §4).
-- Conta DESTINATÁRIOS (é assim que o SES cobra: por destinatário), somados nos três caminhos de
-- envio: outbox (worker), JMAP EmailSubmission direto (proxy) e assistente.
CREATE TABLE tenant_mail_sent (
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    day       DATE NOT NULL,
    sent      INT NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, day)
);

-- +goose Down
DROP TABLE IF EXISTS tenant_mail_sent;
