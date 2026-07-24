//! Limite de largura de banda (token bucket) partilhado por todas as transferências.
//!
//! Um único `Throttle` é partilhado pelas threads de transferência, por isso o limite é GLOBAL (o
//! utilizador configura "não uses mais de X KB/s", não "X KB/s por ficheiro"). `rate = 0` desliga.

use std::io::{self, Read};
use std::sync::Mutex;
use std::time::{Duration, Instant};

pub struct Throttle {
    /// Bytes por segundo. 0 = sem limite (caminho rápido, sem locks).
    rate: u64,
    /// (instante do último refill, tokens disponíveis em bytes)
    state: Mutex<(Instant, f64)>,
}

impl Throttle {
    pub fn new(bytes_per_sec: u64) -> Self {
        Throttle {
            rate: bytes_per_sec,
            state: Mutex::new((Instant::now(), bytes_per_sec as f64)),
        }
    }

    /// Bloqueia até poder "gastar" `n` bytes. Reenche o balde conforme o tempo passado; se não
    /// houver tokens que cheguem, dorme exatamente o tempo em falta.
    pub fn take(&self, n: usize) {
        if self.rate == 0 || n == 0 {
            return;
        }
        let need = n as f64;
        loop {
            let wait = {
                let mut st = self.state.lock().unwrap_or_else(|e| e.into_inner());
                let now = Instant::now();
                let elapsed = now.duration_since(st.0).as_secs_f64();
                st.0 = now;
                // Reenche, com teto de 1 segundo de burst (evita acumular crédito indefinido).
                st.1 = (st.1 + elapsed * self.rate as f64).min(self.rate as f64);
                if st.1 >= need {
                    st.1 -= need;
                    return;
                }
                (need - st.1) / self.rate as f64
            };
            std::thread::sleep(Duration::from_secs_f64(wait.min(1.0)));
        }
    }
}

/// Leitor que respeita o limite de banda — usado tanto no download (corpo da resposta) como no
/// upload (corpo do pedido), para o limite valer nos dois sentidos.
pub struct ThrottledReader<'a, R: Read> {
    inner: R,
    throttle: &'a Throttle,
}

impl<'a, R: Read> ThrottledReader<'a, R> {
    pub fn new(inner: R, throttle: &'a Throttle) -> Self {
        ThrottledReader { inner, throttle }
    }
}

impl<R: Read> Read for ThrottledReader<'_, R> {
    fn read(&mut self, buf: &mut [u8]) -> io::Result<usize> {
        // Limita o tamanho do pedido de leitura para o atraso ser distribuído (e não em blocos
        // enormes), mantendo o débito suave.
        let cap = if self.throttle.rate == 0 {
            buf.len()
        } else {
            buf.len().min(64 * 1024)
        };
        let n = self.inner.read(&mut buf[..cap])?;
        self.throttle.take(n);
        Ok(n)
    }
}
