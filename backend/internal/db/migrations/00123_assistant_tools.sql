-- +goose Up
-- Ferramentas do assistente desligadas pela organização. Granularidade fina por cima do que já
-- existia (ligar/desligar um agente inteiro, ou uma app do rail): aqui corta-se UMA capacidade —
-- "a IA nunca envia e-mail", por exemplo — e o corte vale para TODOS os agentes, de origem ou
-- criados pelo cliente. É um teto, nunca uma extensão: não dá para conceder o que o agente não tem.
-- Só se guarda o que diverge do padrão; ausência = ligada.
CREATE TABLE assistant_tool_settings (
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    tool      TEXT NOT NULL,
    enabled   BOOLEAN NOT NULL DEFAULT TRUE,
    PRIMARY KEY (tenant_id, tool)
);

-- +goose Down
DROP TABLE assistant_tool_settings;
