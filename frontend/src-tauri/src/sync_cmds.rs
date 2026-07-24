//! Ponte entre a UI (React) e o motor de sincronização embutido.
//!
//! O motor corre numa thread de fundo dentro da própria app — não há segunda aplicação. A UI só
//! chama estes comandos; toda a lógica (watcher, delta, conflitos, lixeira) vive em `mailin_sync`.

use std::sync::Arc;

use mailin_sync::engine::{Engine, Status};
use tauri::State;

/// Guardado no estado do Tauri para os comandos alcançarem o mesmo motor.
pub struct SyncEngine(pub Arc<Engine>);

#[tauri::command]
pub fn sync_status(engine: State<'_, SyncEngine>) -> Status {
    engine.0.status()
}

/// Liga esta máquina à conta (servidor + token). O token vem da UI, que o criou via
/// `POST /api/devices` com a sessão do utilizador — ninguém copia nada à mão. Ainda não sincroniza:
/// é preciso adicionar pelo menos uma pasta.
#[tauri::command]
pub fn sync_connect(
    engine: State<'_, SyncEngine>,
    server_url: String,
    token: String,
) -> Result<(), String> {
    engine
        .0
        .connect(&server_url, &token)
        .map_err(|e| e.to_string())
}

/// Adiciona uma pasta local: cria a pasta correspondente no Drive e começa a sincronizar só o que
/// estiver lá dentro. Devolve o par criado (para a UI mostrar já).
#[tauri::command]
pub fn sync_add_folder(
    engine: State<'_, SyncEngine>,
    local_path: String,
) -> Result<mailin_sync::config::SyncPair, String> {
    engine.0.add_pair(&local_path).map_err(|e| e.to_string())
}

/// Deixa de sincronizar uma pasta (não apaga ficheiros de lado nenhum).
#[tauri::command]
pub fn sync_remove_folder(engine: State<'_, SyncEngine>, local_path: String) -> Result<(), String> {
    engine.0.remove_pair(&local_path).map_err(|e| e.to_string())
}

#[tauri::command]
pub fn sync_now(engine: State<'_, SyncEngine>) {
    engine.0.sync_now();
}

#[tauri::command]
pub fn sync_set_paused(engine: State<'_, SyncEngine>, paused: bool) {
    engine.0.set_paused(paused);
}

/// Limite de banda atual (KB/s; 0 = sem limite).
#[tauri::command]
pub fn sync_bandwidth(engine: State<'_, SyncEngine>) -> u64 {
    engine.0.bandwidth()
}

#[tauri::command]
pub fn sync_set_bandwidth(
    engine: State<'_, SyncEngine>,
    bandwidth_kbps: u64,
) -> Result<(), String> {
    engine
        .0
        .set_bandwidth(bandwidth_kbps)
        .map_err(|e| e.to_string())
}
