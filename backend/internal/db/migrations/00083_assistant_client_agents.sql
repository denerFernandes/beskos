-- +goose Up
-- Agentes criados pelo CLIENTE (fase A3b, §3.2). Os de origem NÃO estão aqui: vivem em código
-- (`internal/assistant/agent.go`) porque são parte do produto e ninguém os altera. Uma linha numa
-- tabela é editável por definição — bastaria um UPDATE para mudar as ferramentas de um agente, que
-- é exatamente a fronteira de segurança que se quer congelar.
--
-- A chave de um agente do cliente é o próprio `id`: um UUID nunca colide com 'assistant', 'inbox'
-- ou 'docs', portanto o mesmo campo do pedido serve para os dois tipos sem prefixos.
CREATE TABLE assistant_agents (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    purpose      TEXT NOT NULL DEFAULT '',   -- uma linha; é o que o encaminhamento lê (§3.2)
    instructions TEXT NOT NULL DEFAULT '',
    -- Lista de permissões. Validada NO SERVIDOR contra o teto de ferramentas a cada gravação: a
    -- interface não é a defesa (§3.2, §6.1 defesa 2).
    tools        TEXT[] NOT NULL DEFAULT '{}',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- Nomes únicos por organização, sem depender de maiúsculas: dois agentes "Vendas" e "vendas" na
-- mesma lista seriam indistinguíveis para quem escolhe.
CREATE UNIQUE INDEX idx_assistant_agents_name ON assistant_agents (tenant_id, lower(name));

-- Disponibilidade dos agentes de ORIGEM por organização. Os administradores controlam se cada um
-- aparece — não o que ele faz (§3.2). Só se guarda o que diverge do padrão: ausência = ligado.
CREATE TABLE assistant_builtin_settings (
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    agent_key TEXT NOT NULL,
    enabled   BOOLEAN NOT NULL DEFAULT TRUE,
    PRIMARY KEY (tenant_id, agent_key)
);

-- +goose Down
DROP TABLE assistant_builtin_settings;
DROP TABLE assistant_agents;
