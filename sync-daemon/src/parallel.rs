//! Pool de workers para as transferências. O trabalho pesado é rede (baixar/enviar), por isso
//! paralelizar dá ganho real mesmo com o índice SQLite serializado por um mutex.
//!
//! Usa `thread::scope` (threads com tempo de vida limitado ao escopo) para poder emprestar
//! referências — `&Client`, `&Index`, `&Mutex<Stats>` — sem `Arc` nem `'static`.

use std::sync::Mutex;

/// Corre `f` sobre todos os `items` com até `workers` threads. Cada worker puxa o próximo item de
/// uma fila partilhada, por isso itens lentos não bloqueiam os outros (ao contrário de dividir em
/// blocos fixos). Com `workers <= 1` corre em série, sem criar threads.
pub fn for_each<T, F>(items: Vec<T>, workers: usize, f: F)
where
    T: Send,
    F: Fn(T) + Sync,
{
    if workers <= 1 || items.len() <= 1 {
        for it in items {
            f(it);
        }
        return;
    }
    let queue = Mutex::new(items.into_iter());
    let f = &f;
    let queue = &queue;
    std::thread::scope(|scope| {
        for _ in 0..workers.min(64) {
            scope.spawn(move || loop {
                // O lock só protege a fila — é largado antes de correr o trabalho.
                let next = queue.lock().unwrap_or_else(|e| e.into_inner()).next();
                match next {
                    Some(item) => f(item),
                    None => break,
                }
            });
        }
    });
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::atomic::{AtomicUsize, Ordering};

    #[test]
    fn runs_every_item_exactly_once() {
        let seen = AtomicUsize::new(0);
        for_each((0..100).collect(), 8, |_i| {
            seen.fetch_add(1, Ordering::Relaxed);
        });
        assert_eq!(seen.load(Ordering::Relaxed), 100);
    }

    #[test]
    fn falls_back_to_serial() {
        let sum = AtomicUsize::new(0);
        for_each(vec![1usize, 2, 3], 1, |i| {
            sum.fetch_add(i, Ordering::Relaxed);
        });
        assert_eq!(sum.load(Ordering::Relaxed), 6);
    }
}
