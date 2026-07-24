-- +goose Up
-- Trabalho em segundo plano do assistente (fase A4, §3.6 e §7.5). Pedidos longos deixam de morrer
-- com o pedido HTTP: a conversa devolve logo um cartão de tarefa e o trabalho continua no worker.
--
-- O estado é guardado a cada passo, para a tarefa poder retomar depois de um reinício.
CREATE TABLE assistant_tasks (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    mail_user_id    UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    conversation_id UUID NOT NULL REFERENCES assistant_conversations(id) ON DELETE CASCADE,
    agent           TEXT NOT NULL,
    -- A mensagem do utilizador. É o pedido, e é o que se reexecuta numa retoma.
    message         TEXT NOT NULL,
    -- queued | running | awaiting_confirmation | done | failed | cancelled
    status          TEXT NOT NULL DEFAULT 'queued',
    steps           JSONB NOT NULL DEFAULT '[]',  -- cartões de ação já feitos (o progresso à vista)
    answer          TEXT NOT NULL DEFAULT '',
    -- Chave de i18n, nunca texto para mostrar cru (lei 4.4/4.6).
    error_code      TEXT NOT NULL DEFAULT '',
    stopped         TEXT NOT NULL DEFAULT '',
    run_id          UUID,
    -- Cancelar é um PEDIDO, não um efeito imediato: a tarefa pode estar a meio de uma chamada. O
    -- worker vê a bandeira no passo seguinte e pára — o que já fez fica registado (§7.5).
    cancel_requested BOOLEAN NOT NULL DEFAULT FALSE,
    -- claimed_at é o arrendamento do worker. Uma tarefa 'running' com arrendamento velho é de um
    -- worker que morreu, e volta à fila.
    claimed_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_assistant_tasks_user ON assistant_tasks (mail_user_id, created_at DESC);
-- A fila do worker: o que está por fazer, mais antigo primeiro.
CREATE INDEX idx_assistant_tasks_queue ON assistant_tasks (status, created_at) WHERE status IN ('queued', 'running');

-- Ações à espera de aprovação humana, DURÁVEIS (§6.3). Na fase A2 a espera vivia em memória, o que
-- chegava para uma execução síncrona — que morre com o servidor de qualquer forma. Uma tarefa em
-- fundo não: fica "a aguardar confirmação" e tem de continuar lá depois de um reinício.
CREATE TABLE assistant_pending_actions (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id      UUID NOT NULL REFERENCES assistant_tasks(id) ON DELETE CASCADE,
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    mail_user_id UUID NOT NULL REFERENCES mail_users(id) ON DELETE CASCADE,
    tool         TEXT NOT NULL,
    -- Os argumentos EXATOS que vão correr. É o que se mostra a quem decide: uma pré-visualização
    -- que divergisse do que corre daria confiança sem a merecer.
    args         JSONB NOT NULL DEFAULT '{}',
    -- '' = à espera. Nunca se assume aprovação por omissão.
    decision     TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    decided_at   TIMESTAMPTZ
);
CREATE INDEX idx_assistant_pending_user ON assistant_pending_actions (mail_user_id, created_at DESC)
    WHERE decision = '';

-- Efeitos já produzidos por uma tarefa. É o que dá IDEMPOTÊNCIA às ferramentas de escrita numa
-- retoma (lei 5.3, §7.5): uma tarefa que reinicia reexecuta o ciclo, e sem isto reenviaria o mesmo
-- e-mail. A impressão digital é (ferramenta + argumentos), portanto o mesmo envio não sai duas
-- vezes — mas dois envios diferentes na mesma tarefa saem os dois, como devem.
CREATE TABLE assistant_task_effects (
    task_id     UUID NOT NULL REFERENCES assistant_tasks(id) ON DELETE CASCADE,
    fingerprint TEXT NOT NULL,
    summary     TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (task_id, fingerprint)
);

-- +goose Down
DROP TABLE assistant_task_effects;
DROP TABLE assistant_pending_actions;
DROP TABLE assistant_tasks;
