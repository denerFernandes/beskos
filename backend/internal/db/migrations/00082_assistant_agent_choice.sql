-- +goose Up
-- Escolha de agente por turno (fase A3, §3.2). O encaminhamento decide a cada mensagem, portanto o
-- agente que respondeu é uma propriedade da MENSAGEM, não da conversa: um fio pode começar no
-- assistente geral e passar pela Caixa de entrada na pergunta seguinte.
--
-- A escolha é sempre visível — daí guardá-la. Encaminhamento silencioso deixaria a pessoa sem
-- perceber porque é que o assistente às vezes consegue uma coisa e às vezes não.
ALTER TABLE assistant_messages ADD COLUMN agent TEXT NOT NULL DEFAULT '';

-- E é reversível: fixar um agente vale para o RESTO DO FIO, até a pessoa desfixar. Vazio = o
-- encaminhamento decide a cada turno.
ALTER TABLE assistant_conversations ADD COLUMN pinned_agent TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE assistant_conversations DROP COLUMN pinned_agent;
ALTER TABLE assistant_messages DROP COLUMN agent;
