-- +goose Up
-- Registo de auditoria de TODA a chamada de ferramenta do assistente de IA (§6.4 do desenho).
-- Existe para responder a "porque é que este e-mail foi enviado?" — sem isto, um agente que age
-- em nome do utilizador é uma caixa preta.
--
-- Notas de desenho:
--   - `run_id` agrupa as chamadas de uma execução (um turno). NÃO é chave estrangeira: as
--     conversas só chegam na fase A1, e o registo tem de funcionar antes disso. A1 acrescenta
--     `conversation_id` como coluna nova, sem mexer nesta.
--   - `result` guarda o RESUMO legível, não o payload todo: o conteúdo dos e-mails lidos não fica
--     duplicado numa segunda tabela (privacidade e tamanho).
--   - o registo é legível pelo próprio utilizador, não só pelo administrador — daí o índice por
--     mail_user_id.
CREATE TABLE assistant_tool_calls (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    mail_user_id UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    run_id       UUID NOT NULL,                    -- agrupa as chamadas de um turno
    agent        TEXT NOT NULL,                    -- chave do agente (ex.: 'assistant')
    tool         TEXT NOT NULL,                    -- nome da ferramenta (ex.: 'mail.search')
    args         JSONB NOT NULL DEFAULT '{}',      -- argumentos como o modelo os produziu
    result       TEXT NOT NULL DEFAULT '',         -- resumo do que aconteceu, legível
    error        TEXT NOT NULL DEFAULT '',         -- vazio = sucesso
    duration_ms  INTEGER NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- Registo do próprio utilizador, mais recente primeiro (o ecrã de auditoria da A7).
CREATE INDEX idx_assistant_tool_calls_user ON assistant_tool_calls (mail_user_id, created_at DESC);
-- Todas as chamadas de uma execução, por ordem (os cartões de ação de uma conversa).
CREATE INDEX idx_assistant_tool_calls_run ON assistant_tool_calls (run_id, created_at);

-- +goose Down
DROP TABLE assistant_tool_calls;
