//! Configuração do daemon. O segredo (token de dispositivo) fica NUM ficheiro à parte com permissões
//! 0600 (só o dono lê), isolado da config não-sensível. Mesma postura de ~/.ssh e ~/.aws/credentials.
//! (Hardening futuro: mover o token para o keychain do SO via `keyring`.)

use std::fs;
use std::path::{Path, PathBuf};

use anyhow::{Context, Result};
use serde::{Deserialize, Serialize};

/// Um par sincronizado: uma pasta LOCAL espelhada com uma pasta da nuvem. Só o que está dentro
/// deste par sincroniza — nada de espelhar o drive inteiro. Podem existir vários.
#[derive(Serialize, Deserialize, Clone)]
pub struct SyncPair {
    /// Caminho absoluto da pasta no computador.
    pub local_path: String,
    /// Id da pasta correspondente no Drive (criada quando o par foi adicionado).
    pub folder_id: String,
    /// Nome da pasta na nuvem (só para mostrar na UI).
    pub name: String,
}

#[derive(Serialize, Deserialize, Clone)]
pub struct Config {
    pub server_url: String,
    /// Identidade da CONTA a que esta config pertence (ex.: id/e-mail do utilizador). Isola a config
    /// por conta na mesma máquina: ao trocar de utilizador, os pares de outra conta não são mostrados
    /// nem herdados. Vazio = config antiga (pré-isolamento) — nesse caso compara-se só o server_url.
    #[serde(default)]
    pub account: String,
    /// Pares de pastas sincronizadas. Vazio = nada sincroniza (é preciso adicionar uma pasta).
    #[serde(default)]
    pub pairs: Vec<SyncPair>,
    /// Transferências em paralelo. 0/ausente = 4.
    #[serde(default)]
    pub workers: usize,
    /// Teto de largura de banda em KB/s (soma de subidas e descidas). 0/ausente = sem limite.
    #[serde(default)]
    pub bandwidth_kbps: u64,
}

impl Config {
    /// Nº de threads de transferência (com default e teto sensatos).
    pub fn workers(&self) -> usize {
        if self.workers == 0 {
            4
        } else {
            self.workers.min(32)
        }
    }
    /// Limite em bytes/s (0 = sem limite).
    pub fn bandwidth_bps(&self) -> u64 {
        self.bandwidth_kbps.saturating_mul(1024)
    }
}

/// Diretório de config do daemon: $MAILIN_SYNC_HOME, senão o padrão da plataforma sob $HOME.
pub fn config_dir() -> Result<PathBuf> {
    if let Ok(h) = std::env::var("MAILIN_SYNC_HOME") {
        return Ok(PathBuf::from(h));
    }
    let home = std::env::var("HOME").context("HOME não definido")?;
    let base = if cfg!(target_os = "macos") {
        Path::new(&home).join("Library/Application Support/mailin-sync")
    } else {
        Path::new(&home).join(".config/mailin-sync")
    };
    Ok(base)
}

fn config_path() -> Result<PathBuf> {
    Ok(config_dir()?.join("config.json"))
}

fn token_path() -> Result<PathBuf> {
    Ok(config_dir()?.join("token"))
}

pub fn load() -> Result<Config> {
    let raw = fs::read_to_string(config_path()?)
        .context("config não encontrada — corra `mailin-sync login` primeiro")?;
    Ok(serde_json::from_str(&raw)?)
}

pub fn load_token() -> Result<String> {
    let t =
        fs::read_to_string(token_path()?).context("token ausente — corra `mailin-sync login`")?;
    Ok(t.trim().to_string())
}

/// Grava config + token. O token vai para um ficheiro 0600; a config para 0644.
pub fn save(cfg: &Config, token: &str) -> Result<()> {
    let dir = config_dir()?;
    fs::create_dir_all(&dir)?;
    restrict_dir(&dir);

    fs::write(config_path()?, serde_json::to_vec_pretty(cfg)?)?;

    let tp = token_path()?;
    fs::write(&tp, token)?;
    restrict_file(&tp);
    Ok(())
}

#[cfg(unix)]
fn restrict_file(p: &Path) {
    use std::os::unix::fs::PermissionsExt;
    let _ = fs::set_permissions(p, fs::Permissions::from_mode(0o600));
}

#[cfg(unix)]
fn restrict_dir(p: &Path) {
    use std::os::unix::fs::PermissionsExt;
    let _ = fs::set_permissions(p, fs::Permissions::from_mode(0o700));
}

#[cfg(not(unix))]
fn restrict_file(_p: &Path) {}

#[cfg(not(unix))]
fn restrict_dir(_p: &Path) {}
