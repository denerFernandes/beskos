-- +goose Up
-- O link público (file_links) deixa de ser só "descarregar": passa a ABRIR o ficheiro (OnlyOffice,
-- sessão anónima), em modo ver ou editar, com a descarga como opção separada — o modelo do Google.
--   permission: 'view' (só ler) | 'edit' (editar anonimamente). Default 'view' (o mais seguro).
--   allow_download: se quem abre pode descarregar. Default true para os links já existentes, que
--                   eram exatamente links de descarga, continuarem a funcionar como antes.
ALTER TABLE file_links ADD COLUMN permission TEXT NOT NULL DEFAULT 'view';
ALTER TABLE file_links ADD COLUMN allow_download BOOLEAN NOT NULL DEFAULT true;

-- +goose Down
ALTER TABLE file_links DROP COLUMN permission;
ALTER TABLE file_links DROP COLUMN allow_download;
