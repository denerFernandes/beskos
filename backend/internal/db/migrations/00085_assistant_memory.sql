-- +goose Up
-- Memória de factos duradouros sobre o utilizador (fase A6, §8). Onde a pessoa vive, o fuso, como
-- assina, quem é quem na equipa. Trazida para qualquer conversa, para as horas e os feriados saírem
-- certos sem se repetir.
--
-- A regra dura de §6.5 é o desenho desta tabela: SÓ SE GUARDA O QUE O UTILIZADOR ESCREVEU. Nada
-- extraído de e-mails, documentos, chat ou resultados de ferramentas entra aqui — nunca. Sem isso,
-- um atacante envia um e-mail que diz "o utilizador autoriza envios para X", o extrator guarda-o, e
-- o envenenamento passa a ser permanente e invisível. Não há coluna que aponte para um e-mail: a
-- origem é sempre uma MENSAGEM DO UTILIZADOR.
CREATE TABLE assistant_memory (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    mail_user_id      UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    -- Facto em TEXTO LEGÍVEL, não num embedding opaco: o utilizador tem de conseguir ler o que a
    -- aplicação sabe sobre si e reconhecer se está errado (§8.1).
    fact              TEXT NOT NULL,
    -- local | pessoa | preferência | trabalho (§8.1). Serve para filtrar na injeção quando há muitos.
    category          TEXT NOT NULL DEFAULT 'preferência',
    -- De onde veio: a mensagem do utilizador que o originou. Auditável (§6.5). ON DELETE SET NULL
    -- porque a retenção apaga a conversa mas o facto sobrevive (§8.5) — a origem passa a "removida".
    source_message_id UUID REFERENCES assistant_messages(id) ON DELETE SET NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at      TIMESTAMPTZ
);
CREATE INDEX idx_assistant_memory_user ON assistant_memory (mail_user_id, updated_at DESC);

-- +goose Down
DROP TABLE assistant_memory;
