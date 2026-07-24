-- +goose Up
-- Conversas persistidas do assistente (fase A1 do desenho, §7.4). O que a A0 fazia num pedido HTTP
-- e esquecia, passa a ter fio: cada conversa é um histórico reabrível, e o contexto do fio (§3.5.2)
-- volta ao modelo nas execuções seguintes.
--
-- Notas de desenho:
--   - `mail_user_id` + `tenant_id` são o filtro obrigatório (lei 5.1). A conversa é PESSOAL: não é
--     partilhada entre utilizadores do mesmo tenant (§3.5).
--   - `agent` guarda a chave do perfil que respondeu. Cada agente tem as suas conversas (§3.2) e
--     ver qual respondeu é um requisito, não um extra.
--   - `title` é derivado da primeira mensagem do utilizador — não do modelo. Um título gerado por
--     IA custaria uma chamada por conversa e diria menos do que a própria frase.
CREATE TABLE assistant_conversations (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    mail_user_id UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    agent        TEXT NOT NULL,
    title        TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- Lista do próprio utilizador, mais recente primeiro.
CREATE INDEX idx_assistant_conversations_user ON assistant_conversations (mail_user_id, updated_at DESC);

-- Mensagens de uma conversa. Inclui os cartões de ação (§3.3): o registo da chamada real fica com a
-- mensagem que a originou, para que reabrir a conversa mostre o mesmo que se viu ao vivo.
--
-- `steps` guarda o RESUMO de cada chamada, não o payload do que foi lido — o corpo dos e-mails não
-- fica duplicado numa tabela nossa (privacidade e tamanho). É a mesma escolha de `assistant_tool_calls`.
CREATE TABLE assistant_messages (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    conversation_id UUID NOT NULL REFERENCES assistant_conversations(id) ON DELETE CASCADE,
    role            TEXT NOT NULL,                  -- 'user' | 'assistant'
    content         TEXT NOT NULL DEFAULT '',
    steps           JSONB NOT NULL DEFAULT '[]',    -- cartões de ação desta resposta
    stopped         TEXT NOT NULL DEFAULT '',       -- motivo de paragem (chave de i18n), vazio = respondeu
    run_id          UUID,                           -- liga aos registos de auditoria da execução
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_assistant_messages_conv ON assistant_messages (conversation_id, created_at);

-- A A0 deixou o registo de auditoria sem conversa de propósito (as conversas não existiam). Agora
-- existe: a coluna é ANULÁVEL porque as linhas escritas na A0 não têm conversa nenhuma a apontar.
ALTER TABLE assistant_tool_calls
    ADD COLUMN conversation_id UUID REFERENCES assistant_conversations(id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE assistant_tool_calls DROP COLUMN conversation_id;
DROP TABLE assistant_messages;
DROP TABLE assistant_conversations;
