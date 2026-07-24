-- +goose Up
-- Apresentações do Meet (estilo BigBlueButton): um PDF por sessão de sala, com o slide atual
-- partilhado. PPTX/PPT/ODP são convertidos para PDF (OnlyOffice) antes de gravar aqui; o object_key
-- aponta para o PDF final no MinIO. A anotação (caneta/laser) trafega em tempo real pelo data
-- channel do LiveKit — não é persistida.
CREATE TABLE meet_presentations (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    room          text NOT NULL,
    tenant_id     uuid NOT NULL,
    name          text NOT NULL,
    object_key    text NOT NULL,               -- chave do PDF no MinIO
    current_slide integer NOT NULL DEFAULT 0,
    active        boolean NOT NULL DEFAULT true,
    created_by    uuid NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now()
);

-- Uma apresentação ativa por sala (a última a começar). O índice acelera o lookup por sala.
CREATE INDEX meet_presentations_room_active ON meet_presentations (room) WHERE active;

-- +goose Down
DROP TABLE meet_presentations;
