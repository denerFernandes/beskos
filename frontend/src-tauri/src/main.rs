// Impede uma janela de consola extra no Windows em release.
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

fn main() {
    mailin_lib::run()
}
