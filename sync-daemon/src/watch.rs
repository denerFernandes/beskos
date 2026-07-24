//! Watcher do sistema de ficheiros: em vez de esperar o intervalo de polling, reage a mudanças na
//! pasta local quase de imediato (FSEvents no macOS, inotify no Linux, ReadDirectoryChanges no
//! Windows — via `notify`).
//!
//! Os eventos são **coalescidos**: uma cópia de 500 ficheiros dispara centenas de eventos, mas só
//! queremos UMA passagem de sincronização no fim. Esperamos um período de silêncio antes de acordar
//! a thread de sync. O próprio download do agente também gera eventos — a passagem seguinte não
//! encontra nada por fazer (mtime/size batem) e assenta sozinha.

use std::path::PathBuf;
use std::sync::mpsc::{self, Sender};
use std::time::Duration;

use anyhow::Result;
use notify::{RecommendedWatcher, RecursiveMode, Watcher};

/// Tempo de silêncio antes de considerar que a rajada de eventos acabou.
const QUIET: Duration = Duration::from_millis(1500);

/// Observa `root` e envia um "nudge" quando o disco muda (já coalescido).
/// **O watcher devolvido tem de ficar vivo** — se for descartado, deixa de observar.
pub fn spawn(root: PathBuf, nudge: Sender<()>) -> Result<RecommendedWatcher> {
    let (tx, rx) = mpsc::channel();
    let mut watcher = notify::recommended_watcher(move |res| {
        let _ = tx.send(res);
    })?;
    watcher.watch(&root, RecursiveMode::Recursive)?;

    std::thread::spawn(move || {
        loop {
            // Bloqueia até ao primeiro evento da rajada.
            if rx.recv().is_err() {
                break; // watcher caiu
            }
            // Drena o resto da rajada até haver QUIET de silêncio.
            while rx.recv_timeout(QUIET).is_ok() {}
            if nudge.send(()).is_err() {
                break; // ninguém a ouvir: a app está a fechar
            }
        }
    });

    Ok(watcher)
}
