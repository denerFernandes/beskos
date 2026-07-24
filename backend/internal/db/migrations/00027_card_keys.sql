-- +goose Up
-- Tarefas com identificador estilo Jira: PREFIXO-NÚMERO (ex.: QUA-12), por quadro.
--
-- O número é por QUADRO, não global: é o que faz "QUA-1" ser o primeiro card do quadro Quadro,
-- independentemente do que acontece nos outros. Mesmo mecanismo de sequência dos antigos chamados:
-- UPDATE ... RETURNING trava a linha do quadro, então dois cards criados ao mesmo tempo nunca
-- recebem o mesmo número (MAX(number)+1 daria empate).

ALTER TABLE boards ADD COLUMN prefix   TEXT NOT NULL DEFAULT '';
ALTER TABLE boards ADD COLUMN card_seq BIGINT NOT NULL DEFAULT 0;
ALTER TABLE cards  ADD COLUMN number   BIGINT;

-- Prefixo padrão: as 3 primeiras letras/dígitos do nome, em maiúsculas ("Quadro" → QUA).
-- Nome sem letras nenhumas (ex.: "🚀") cairia em '' — daí o COALESCE para 'TSK'.
UPDATE boards SET prefix = COALESCE(
    NULLIF(upper(substring(regexp_replace(name, '[^a-zA-Z0-9]', '', 'g') from 1 for 3)), ''),
    'TSK');

-- Numera os cards que já existem por ordem de criação, para o mais antigo ser o #1.
WITH numbered AS (
    SELECT id, row_number() OVER (PARTITION BY board_id ORDER BY created_at, id) AS n FROM cards
)
UPDATE cards c SET number = numbered.n FROM numbered WHERE numbered.id = c.id;

-- O contador de cada quadro continua de onde a numeração parou.
UPDATE boards b SET card_seq = COALESCE((SELECT max(c.number) FROM cards c WHERE c.board_id = b.id), 0);

ALTER TABLE cards ALTER COLUMN number SET NOT NULL;
ALTER TABLE cards ADD CONSTRAINT cards_number_uq UNIQUE (board_id, number);

-- +goose Down
ALTER TABLE cards DROP CONSTRAINT cards_number_uq;
ALTER TABLE cards DROP COLUMN number;
ALTER TABLE boards DROP COLUMN card_seq;
ALTER TABLE boards DROP COLUMN prefix;
