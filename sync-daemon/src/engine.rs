//! O motor embutível: uma thread de fundo que sincroniza, um watcher que a acorda quando a pasta
//! muda, e um estado observável para a UI (app desktop ou ícone na barra) mostrar ao utilizador.
//!
//! Foi extraído do modo agente para o mesmo código servir os dois hospedeiros — a app desktop e o
//! binário headless — sem duplicar lógica.

use std::sync::mpsc::{self, Receiver, Sender};
use std::sync::{Arc, Mutex};
use std::time::Duration;

use anyhow::Result;
use serde::Serialize;

use crate::{api, config, index, sync};

/// Em que ponto está a sincronização (o que a UI mostra).
#[derive(Clone, Copy, PartialEq, Serialize)]
#[serde(rename_all = "lowercase")]
pub enum Phase {
    /// Nunca correu / à espera do próximo ciclo.
    Idle,
    Syncing,
    Error,
    Paused,
}

/// Estado publicado para a UI. É `Serialize` para atravessar a ponte do Tauri tal como está.
#[derive(Clone, Serialize)]
pub struct Status {
    pub phase: Phase,
    /// Resumo da última passagem ("3 baixados, 1 enviado") ou a mensagem de erro.
    pub message: String,
    pub paused: bool,
    /// Se já há credenciais gravadas (senão a UI mostra o ecrã de "ligar esta máquina").
    pub configured: bool,
    /// Pastas sincronizadas (pares local↔nuvem).
    pub pairs: Vec<crate::config::SyncPair>,
}

impl Status {
    fn new() -> Self {
        Status {
            phase: Phase::Idle,
            message: String::new(),
            paused: false,
            configured: false,
            pairs: Vec::new(),
        }
    }
}

/// Motor de sincronização a correr em fundo. Enquanto este objeto viver, a thread continua.
pub struct Engine {
    state: Arc<Mutex<Status>>,
    nudge: Sender<()>,
    /// Um watcher por pasta sincronizada. Têm de ficar vivos (se caírem, deixam de observar).
    _watchers: Mutex<Vec<notify::RecommendedWatcher>>,
}

impl Engine {
    /// Arranca o motor. `interval` é a rede de segurança entre passagens (o watcher acorda antes).
    pub fn start(interval: u64) -> Arc<Self> {
        let state = Arc::new(Mutex::new(Status::new()));
        let (tx, rx) = mpsc::channel();

        let engine = Arc::new(Engine {
            state: Arc::clone(&state),
            nudge: tx.clone(),
            _watchers: Mutex::new(Vec::new()),
        });

        spawn_loop(Arc::clone(&state), rx, interval);
        engine.refresh_config();
        engine.rearm_watcher();
        engine
    }

    /// Estado atual (para a UI).
    pub fn status(&self) -> Status {
        self.state.lock().unwrap_or_else(|e| e.into_inner()).clone()
    }

    /// Pede uma sincronização já, sem esperar o intervalo.
    pub fn sync_now(&self) {
        let _ = self.nudge.send(());
    }

    pub fn set_paused(&self, paused: bool) {
        let mut s = self.state.lock().unwrap_or_else(|e| e.into_inner());
        s.paused = paused;
        s.phase = if paused { Phase::Paused } else { Phase::Idle };
    }

    /// Relê a config do disco para o estado (depois de a UI a mudar) e reaponta os watchers.
    pub fn refresh_config(&self) {
        let cfg = config::load().ok();
        let has_token = config::load_token().map(|t| !t.is_empty()).unwrap_or(false);
        let mut s = self.state.lock().unwrap_or_else(|e| e.into_inner());
        s.configured = cfg.is_some() && has_token;
        s.pairs = cfg.map(|c| c.pairs).unwrap_or_default();
    }

    /// (Re)aponta um watcher por cada pasta sincronizada.
    pub fn rearm_watcher(&self) {
        let pairs = self.status().pairs;
        let mut watchers = Vec::new();
        for p in pairs {
            match crate::watch::spawn(std::path::PathBuf::from(&p.local_path), self.nudge.clone()) {
                Ok(w) => watchers.push(w),
                Err(e) => eprintln!("aviso: watcher indisponível em {} ({e})", p.local_path),
            }
        }
        *self._watchers.lock().unwrap_or_else(|e| e.into_inner()) = watchers;
    }

    /// Liga esta máquina à conta (servidor + token do dispositivo). Ainda não sincroniza nada —
    /// é preciso adicionar pelo menos uma pasta com `add_pair`.
    pub fn connect(&self, server_url: &str, token: &str) -> Result<()> {
        let _ = api::Client::new(server_url, token)?; // valida cedo (exige https fora de localhost)
        let prev = config::load().ok();
        config::save(
            &config::Config {
                server_url: server_url.to_string(),
                pairs: prev.as_ref().map(|c| c.pairs.clone()).unwrap_or_default(),
                workers: prev.as_ref().map(|c| c.workers).unwrap_or(0),
                bandwidth_kbps: prev.as_ref().map(|c| c.bandwidth_kbps).unwrap_or(0),
            },
            token,
        )?;
        self.refresh_config();
        self.rearm_watcher();
        Ok(())
    }

    /// Adiciona uma pasta local à sincronização: **cria a pasta correspondente no Drive** (com o
    /// nome da pasta local) e passa a sincronizar só o que está lá dentro, nos dois sentidos.
    pub fn add_pair(&self, local_path: &str) -> Result<crate::config::SyncPair> {
        let mut cfg = config::load()?;
        let token = config::load_token()?;
        if cfg.pairs.iter().any(|p| p.local_path == local_path) {
            anyhow::bail!("esta pasta já está a ser sincronizada");
        }
        std::fs::create_dir_all(local_path)?;
        let name = std::path::Path::new(local_path)
            .file_name()
            .map(|n| n.to_string_lossy().to_string())
            .unwrap_or_else(|| "Sincronizada".to_string());

        let client = api::Client::with_bandwidth(&cfg.server_url, &token, cfg.bandwidth_bps())?;
        // Reaproveita uma pasta de topo com o mesmo nome, se já existir; senão cria-a.
        let existing = client
            .folders()?
            .into_iter()
            .find(|f| f.parent_id.is_none() && f.name == name);
        let folder = match existing {
            Some(f) => f,
            None => client.create_folder(&name, None)?,
        };

        let pair = crate::config::SyncPair {
            local_path: local_path.to_string(),
            folder_id: folder.id,
            name,
        };
        cfg.pairs.push(pair.clone());
        config::save(&cfg, &token)?;
        self.refresh_config();
        self.rearm_watcher();
        self.sync_now();
        Ok(pair)
    }

    /// Deixa de sincronizar uma pasta. NÃO apaga ficheiros — nem locais nem na nuvem.
    pub fn remove_pair(&self, local_path: &str) -> Result<()> {
        let mut cfg = config::load()?;
        let token = config::load_token()?;
        cfg.pairs.retain(|p| p.local_path != local_path);
        config::save(&cfg, &token)?;
        self.refresh_config();
        self.rearm_watcher();
        Ok(())
    }

    /// Limite de banda atual (KB/s).
    pub fn bandwidth(&self) -> u64 {
        config::load().map(|c| c.bandwidth_kbps).unwrap_or(0)
    }

    pub fn set_bandwidth(&self, bandwidth_kbps: u64) -> Result<()> {
        let mut cfg = config::load()?;
        cfg.bandwidth_kbps = bandwidth_kbps;
        let token = config::load_token()?;
        config::save(&cfg, &token)?;
        Ok(())
    }
}

fn index_db_path() -> Result<std::path::PathBuf> {
    Ok(config::config_dir()?.join("index.db"))
}

/// A thread de fundo: sincroniza, publica o estado, e dorme até ao próximo ciclo ou até ser acordada.
fn spawn_loop(state: Arc<Mutex<Status>>, nudge: Receiver<()>, interval: u64) {
    std::thread::spawn(move || loop {
        let (paused, configured) = {
            let s = state.lock().unwrap_or_else(|e| e.into_inner());
            (s.paused, s.configured)
        };
        if !paused && configured {
            {
                let mut s = state.lock().unwrap_or_else(|e| e.into_inner());
                s.phase = Phase::Syncing;
            }
            // Recarrega config/índice a cada passagem: mudar a pasta ou o token na UI passa a
            // valer sem reiniciar a app.
            let outcome = (|| -> Result<sync::Stats> {
                let cfg = config::load()?;
                let token = config::load_token()?;
                let client =
                    api::Client::with_bandwidth(&cfg.server_url, &token, cfg.bandwidth_bps())?;
                let idx = index::Index::open(&index_db_path()?)?;
                sync::run_once(&client, &idx, &cfg.pairs, cfg.workers())
            })();
            let mut s = state.lock().unwrap_or_else(|e| e.into_inner());
            match outcome {
                Ok(st) => {
                    s.phase = Phase::Idle;
                    s.message = format!(
                        "{} baixados, {} enviados, {} movidos, {} p/ lixeira, {} conflitos",
                        st.downloaded, st.uploaded, st.moved, st.trashed, st.conflicts
                    );
                }
                Err(e) => {
                    s.phase = Phase::Error;
                    s.message = format!("{e}");
                }
            }
        }
        let _ = nudge.recv_timeout(Duration::from_secs(interval));
    });
}
