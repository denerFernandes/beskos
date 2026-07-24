//! Índice local em SQLite: regista os ficheiros que ESTE daemon baixou (id do servidor → caminho
//! relativo + hash + versão) e o cursor do feed. É a fonte de verdade para saber o que já está em
//! disco e — crucial para segurança — o que o daemon pode apagar (só o que ele próprio criou; nunca
//! toca em ficheiros locais desconhecidos).

use std::sync::Mutex;

use anyhow::Result;
use rusqlite::{params, Connection};

/// A `Connection` do rusqlite é `Send` mas não `Sync`, por isso fica atrás de um `Mutex`: assim um
/// `&Index` pode ser partilhado pelas threads de transferência. Serializar o SQLite não é problema —
/// o gargalo é a rede, e as transferências correm em paralelo fora deste lock.
pub struct Index {
    conn: Mutex<Connection>,
}

pub struct Entry {
    pub rel_path: String,
    pub hash: String,
}

pub struct Tracked {
    pub file_id: String,
    pub rel_path: String,
    pub hash: String,
    /// mtime (epoch s) e tamanho na última sincronização — pré-filtro barato: se ambos batem com o
    /// disco, o conteúdo não mudou e nem sequer é preciso ler/hashear o ficheiro.
    pub mtime: i64,
    pub size: i64,
}

impl Index {
    /// Acesso serializado à ligação. `unwrap_or_else` recupera de um lock envenenado (uma thread que
    /// entrou em pânico não deve deixar o índice inutilizável para sempre).
    fn lock(&self) -> std::sync::MutexGuard<'_, Connection> {
        self.conn.lock().unwrap_or_else(|e| e.into_inner())
    }

    pub fn open(path: &std::path::Path) -> Result<Self> {
        let conn = Connection::open(path)?;
        conn.execute_batch(
            "CREATE TABLE IF NOT EXISTS files (
                file_id  TEXT PRIMARY KEY,
                rel_path TEXT NOT NULL,
                hash     TEXT NOT NULL,
                version  INTEGER NOT NULL,
                mtime    INTEGER NOT NULL DEFAULT 0,
                size     INTEGER NOT NULL DEFAULT 0
             );
             CREATE TABLE IF NOT EXISTS meta (
                key TEXT PRIMARY KEY,
                val TEXT NOT NULL
             );",
        )?;
        // Índices criados antes do pré-filtro não têm as colunas: acrescenta-as (erro = já existem).
        let _ = conn.execute(
            "ALTER TABLE files ADD COLUMN mtime INTEGER NOT NULL DEFAULT 0",
            [],
        );
        let _ = conn.execute(
            "ALTER TABLE files ADD COLUMN size INTEGER NOT NULL DEFAULT 0",
            [],
        );
        // O índice é só uma CACHE local. Quando o modelo de sincronização muda (aqui: de espelhar o
        // drive inteiro para pares de pastas), os caminhos guardados deixam de fazer sentido —
        // recomeça-se do zero em vez de arrastar entradas inválidas.
        const SCHEMA: &str = "2-pairs";
        let ver: Option<String> = conn
            .query_row("SELECT val FROM meta WHERE key='schema'", [], |r| r.get(0))
            .ok();
        if ver.as_deref() != Some(SCHEMA) {
            conn.execute("DELETE FROM files", [])?;
            conn.execute(
                "DELETE FROM meta WHERE key IN ('cursor','last_reconcile')",
                [],
            )?;
            conn.execute(
                "INSERT INTO meta(key,val) VALUES('schema',?1)
                 ON CONFLICT(key) DO UPDATE SET val=excluded.val",
                params![SCHEMA],
            )?;
        }
        Ok(Self {
            conn: Mutex::new(conn),
        })
    }

    pub fn cursor(&self) -> Result<String> {
        let v: Option<String> = self
            .lock()
            .query_row("SELECT val FROM meta WHERE key='cursor'", [], |r| r.get(0))
            .ok();
        Ok(v.unwrap_or_default())
    }

    pub fn set_cursor(&self, cursor: &str) -> Result<()> {
        self.lock().execute(
            "INSERT INTO meta(key,val) VALUES('cursor',?1)
             ON CONFLICT(key) DO UPDATE SET val=excluded.val",
            params![cursor],
        )?;
        Ok(())
    }

    /// Lê/escreve um valor arbitrário em `meta` (ex.: quando foi a última reconciliação completa).
    pub fn meta_get(&self, key: &str) -> Option<String> {
        self.lock()
            .query_row("SELECT val FROM meta WHERE key=?1", params![key], |r| {
                r.get(0)
            })
            .ok()
    }

    pub fn meta_set(&self, key: &str, val: &str) -> Result<()> {
        self.lock().execute(
            "INSERT INTO meta(key,val) VALUES(?1,?2)
             ON CONFLICT(key) DO UPDATE SET val=excluded.val",
            params![key, val],
        )?;
        Ok(())
    }

    /// Devolve o registo de um ficheiro, se o daemon já o baixou.
    pub fn get(&self, file_id: &str) -> Result<Option<Entry>> {
        let row = self
            .lock()
            .query_row(
                "SELECT rel_path, hash FROM files WHERE file_id=?1",
                params![file_id],
                |r| {
                    Ok(Entry {
                        rel_path: r.get(0)?,
                        hash: r.get(1)?,
                    })
                },
            )
            .ok();
        Ok(row)
    }

    pub fn upsert(
        &self,
        file_id: &str,
        rel_path: &str,
        hash: &str,
        version: i64,
        mtime: i64,
        size: i64,
    ) -> Result<()> {
        self.lock().execute(
            "INSERT INTO files(file_id,rel_path,hash,version,mtime,size) VALUES(?1,?2,?3,?4,?5,?6)
             ON CONFLICT(file_id) DO UPDATE SET rel_path=excluded.rel_path, hash=excluded.hash,
                 version=excluded.version, mtime=excluded.mtime, size=excluded.size",
            params![file_id, rel_path, hash, version, mtime, size],
        )?;
        Ok(())
    }

    /// Atualiza só o carimbo (mtime/size) — o conteúdo é o mesmo, foi só um "touch" no ficheiro.
    /// Evita voltar a ler e hashear o ficheiro em todas as passagens seguintes.
    pub fn touch_stat(&self, file_id: &str, mtime: i64, size: i64) -> Result<()> {
        self.lock().execute(
            "UPDATE files SET mtime=?2, size=?3 WHERE file_id=?1",
            params![file_id, mtime, size],
        )?;
        Ok(())
    }

    /// Todos os ficheiros rastreados (para a passagem de upload comparar com o disco).
    pub fn all(&self) -> Result<Vec<Tracked>> {
        let conn = self.lock();
        let mut stmt = conn.prepare("SELECT file_id, rel_path, hash, mtime, size FROM files")?;
        let rows = stmt.query_map([], |r| {
            Ok(Tracked {
                file_id: r.get(0)?,
                rel_path: r.get(1)?,
                hash: r.get(2)?,
                mtime: r.get(3)?,
                size: r.get(4)?,
            })
        })?;
        let mut out = Vec::new();
        for row in rows {
            out.push(row?);
        }
        Ok(out)
    }

    pub fn remove(&self, file_id: &str) -> Result<()> {
        self.lock()
            .execute("DELETE FROM files WHERE file_id=?1", params![file_id])?;
        Ok(())
    }

    pub fn count(&self) -> Result<i64> {
        Ok(self
            .lock()
            .query_row("SELECT COUNT(*) FROM files", [], |r| r.get(0))?)
    }
}
