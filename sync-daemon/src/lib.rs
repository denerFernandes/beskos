//! Motor de sincronização do Beskos, como **biblioteca** — para correr dentro da app desktop
//! (Beskos.app) e não como uma segunda aplicação — a app hospeda o motor, o tray e a UI.
//!
//! O ponto de entrada para embeber é o [`engine::Engine`]: arranca uma thread de fundo que
//! sincroniza, reage ao watcher do sistema de ficheiros e publica o estado para a UI ler.

pub mod api;
pub mod config;
pub mod engine;
pub mod index;
pub mod parallel;
pub mod safepath;
pub mod sync;
pub mod throttle;
pub mod tree;
pub mod watch;
