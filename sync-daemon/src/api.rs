//! Cliente HTTP do daemon contra `/api/sync/*`, autenticado pelo token de dispositivo.
//! Segurança: exige HTTPS exceto em localhost (dev); o token nunca é registado em log.

use std::io::Read;

use anyhow::{bail, Context, Result};
use serde::Deserialize;

// folder_id/size/updated_at are part of the feed contract but are not read yet
// (folder_id will drive the tree mirror; the others are for future telemetry/UX).
#[derive(Deserialize, Debug, Clone)]
#[allow(dead_code)]
pub struct FileChange {
    pub id: String,
    #[serde(default)]
    pub folder_id: Option<String>,
    pub name: String,
    pub hash: String,
    pub size: i64,
    #[serde(default)]
    pub version: i64,
    pub deleted: bool,
    pub updated_at: String,
    /// false = partilhado comigo (não sou dono). Tratado como SÓ-LEITURA pelo agente.
    /// Default `true` para continuar a funcionar com servidores anteriores a este campo.
    #[serde(default = "default_true")]
    pub owned: bool,
}

fn default_true() -> bool {
    true
}

/// Metadados devolvidos pelo servidor após criar/atualizar um ficheiro (a linha storage_files).
#[derive(Deserialize)]
pub struct FileMeta {
    pub id: String,
    pub hash: String,
    #[serde(default)]
    pub versao: i64,
}

/// Uma pasta na árvore do utilizador (para o espelho de subpastas).
#[derive(Deserialize, Clone)]
pub struct Folder {
    pub id: String,
    #[serde(default)]
    pub parent_id: Option<String>,
    pub name: String,
}

#[derive(Deserialize)]
struct FoldersPage {
    folders: Vec<Folder>,
}

/// Mapa de blocos do conteúdo atual no servidor (base do upload delta).
#[derive(Deserialize)]
pub struct BlocksInfo {
    pub block_size: u64,
    #[allow(dead_code)]
    pub total_size: u64,
    /// Hash do ficheiro INTEIRO — serve para confirmar que o delta é calculado sobre a mesma base.
    pub hash: String,
    pub hashes: Vec<String>,
}

/// Resultado de um upload condicional (PUT com If-Match).
pub enum PutOutcome {
    Updated(FileMeta),
    Conflict, // 412: o servidor mudou desde a base — o daemon guarda cópia de conflito.
}

#[derive(Deserialize)]
pub struct ChangesPage {
    pub changes: Vec<FileChange>,
    pub cursor: String,
    pub has_more: bool,
}

pub struct Client {
    base: String,
    token: String,
    /// Limite de banda partilhado por todas as transferências deste cliente.
    throttle: crate::throttle::Throttle,
}

impl Client {
    pub fn new(server_url: &str, token: &str) -> Result<Self> {
        Self::with_bandwidth(server_url, token, 0)
    }

    /// `bytes_per_sec` = 0 desliga o limite.
    pub fn with_bandwidth(server_url: &str, token: &str, bytes_per_sec: u64) -> Result<Self> {
        let base = server_url.trim_end_matches('/').to_string();
        // Exige HTTPS, exceto localhost/127.0.0.1 (dev). Impede enviar o token em claro na rede.
        let is_local = base.starts_with("http://localhost")
            || base.starts_with("http://127.0.0.1")
            || base.starts_with("http://[::1]");
        if !base.starts_with("https://") && !is_local {
            bail!("server_url tem de ser https:// (exceto localhost)");
        }
        Ok(Self {
            base,
            token: token.to_string(),
            throttle: crate::throttle::Throttle::new(bytes_per_sec),
        })
    }

    fn auth(&self, req: ureq::Request) -> ureq::Request {
        req.set("Authorization", &format!("Bearer {}", self.token))
    }

    /// Busca uma página do feed de mudanças a partir do cursor (vazio = início).
    pub fn changes(&self, cursor: &str, limit: u32) -> Result<ChangesPage> {
        let url = format!("{}/api/sync/files/changes", self.base);
        let resp = self
            .auth(ureq::get(&url))
            .query("since", cursor)
            .query("limit", &limit.to_string())
            .call()
            .with_context(|| "GET changes falhou")?;
        Ok(resp.into_json::<ChangesPage>()?)
    }

    /// Baixa o conteúdo de um ficheiro pelo id. Devolve os bytes.
    pub fn download(&self, id: &str) -> Result<Vec<u8>> {
        let url = format!("{}/api/sync/files/{}/download", self.base, id);
        let resp = self
            .auth(ureq::get(&url))
            .call()
            .with_context(|| "download falhou")?;
        let mut buf = Vec::new();
        crate::throttle::ThrottledReader::new(resp.into_reader(), &self.throttle)
            .read_to_end(&mut buf)
            .context("ler corpo do download")?;
        Ok(buf)
    }

    /// Atualiza o conteúdo de um ficheiro existente com controlo de concorrência otimista.
    /// `if_match` é o hash base que o daemon conhecia; 412 do servidor → conflito.
    pub fn update_file(&self, id: &str, if_match: &str, body: &[u8]) -> Result<PutOutcome> {
        let url = format!("{}/api/sync/files/{}", self.base, id);
        let res = self
            .auth(ureq::put(&url))
            .set("If-Match", if_match)
            .set("Content-Type", "application/octet-stream")
            .send(crate::throttle::ThrottledReader::new(
                std::io::Cursor::new(body),
                &self.throttle,
            ));
        match res {
            Ok(resp) => Ok(PutOutcome::Updated(resp.into_json::<FileMeta>()?)),
            // 412 Precondition Failed = o servidor mudou desde a base → conflito (não é erro fatal).
            Err(ureq::Error::Status(412, _)) => Ok(PutOutcome::Conflict),
            Err(e) => Err(anyhow::Error::new(e).context("PUT update falhou")),
        }
    }

    /// Cria um ficheiro novo a partir do conteúdo local.
    pub fn create_file(
        &self,
        name: &str,
        folder_id: Option<&str>,
        body: &[u8],
    ) -> Result<FileMeta> {
        let url = format!("{}/api/sync/files", self.base);
        let mut req = self
            .auth(ureq::post(&url))
            .set("Content-Type", "application/octet-stream")
            .query("name", name);
        if let Some(fid) = folder_id {
            req = req.query("folder_id", fid);
        }
        let resp = req
            .send(crate::throttle::ThrottledReader::new(
                std::io::Cursor::new(body),
                &self.throttle,
            ))
            .with_context(|| "POST create falhou")?;
        Ok(resp.into_json::<FileMeta>()?)
    }

    /// Hashes por bloco do conteúdo ATUAL no servidor (para calcular o delta).
    pub fn file_blocks(&self, id: &str, block_size: u64) -> Result<BlocksInfo> {
        let url = format!("{}/api/sync/files/{}/blocks", self.base, id);
        let resp = self
            .auth(ureq::get(&url))
            .query("block_size", &block_size.to_string())
            .call()
            .with_context(|| "GET blocks falhou")?;
        Ok(resp.into_json::<BlocksInfo>()?)
    }

    /// Envia SÓ os blocos alterados; o servidor reconstrói o ficheiro. `body` é a concatenação dos
    /// blocos de `changed`, pela mesma ordem. If-Match garante que o servidor não mudou entretanto.
    pub fn update_file_delta(
        &self,
        id: &str,
        if_match: &str,
        block_size: u64,
        total_size: u64,
        changed: &[usize],
        body: Vec<u8>,
    ) -> Result<PutOutcome> {
        let url = format!("{}/api/sync/files/{}/delta", self.base, id);
        let list = changed
            .iter()
            .map(|i| i.to_string())
            .collect::<Vec<_>>()
            .join(",");
        let res = self
            .auth(ureq::put(&url))
            .set("If-Match", if_match)
            .set("Content-Type", "application/octet-stream")
            .query("block_size", &block_size.to_string())
            .query("total_size", &total_size.to_string())
            .query("blocks", &list)
            .send(crate::throttle::ThrottledReader::new(
                std::io::Cursor::new(body),
                &self.throttle,
            ));
        match res {
            Ok(resp) => Ok(PutOutcome::Updated(resp.into_json::<FileMeta>()?)),
            Err(ureq::Error::Status(412, _)) => Ok(PutOutcome::Conflict),
            Err(e) => Err(anyhow::Error::new(e).context("PUT delta falhou")),
        }
    }

    /// O ficheiro foi apagado na pasta local → manda para a LIXEIRA do servidor (recuperável).
    pub fn trash_file(&self, id: &str) -> Result<()> {
        let url = format!("{}/api/sync/files/{}/trash", self.base, id);
        self.auth(ureq::post(&url))
            .call()
            .with_context(|| "POST trash falhou")?;
        Ok(())
    }

    /// O ficheiro foi movido/renomeado localmente → espelha no servidor. `folder_id` None = raiz.
    pub fn move_file(&self, id: &str, folder_id: Option<&str>, name: &str) -> Result<FileMeta> {
        let url = format!("{}/api/sync/files/{}/move", self.base, id);
        let mut req = self.auth(ureq::post(&url)).query("name", name);
        if let Some(fid) = folder_id {
            req = req.query("folder_id", fid);
        }
        let resp = req.call().with_context(|| "POST move falhou")?;
        Ok(resp.into_json::<FileMeta>()?)
    }

    /// Toda a árvore de pastas do utilizador.
    pub fn folders(&self) -> Result<Vec<Folder>> {
        let url = format!("{}/api/sync/folders", self.base);
        let resp = self
            .auth(ureq::get(&url))
            .call()
            .with_context(|| "GET folders falhou")?;
        Ok(resp.into_json::<FoldersPage>()?.folders)
    }

    /// Cria uma pasta (só para os segmentos que ainda não existem).
    pub fn create_folder(&self, name: &str, parent_id: Option<&str>) -> Result<Folder> {
        let url = format!("{}/api/sync/folders", self.base);
        let mut req = self.auth(ureq::post(&url)).query("name", name);
        if let Some(pid) = parent_id {
            req = req.query("parent_id", pid);
        }
        let resp = req.call().with_context(|| "POST folder falhou")?;
        Ok(resp.into_json::<Folder>()?)
    }
}
