//! Reconciliação bidirecional (M2) com espelho de subpastas. A ORDEM importa: **push antes de pull**.
//!   1. push: sobe mudanças locais com If-Match (o hash base). Se o servidor também mudou → 412,
//!      guardamos uma cópia de conflito local e deixamos o pull trazer a versão do servidor. Push
//!      antes de pull garante que uma edição local não é sobrescrita pelo download antes de subir.
//!      Ficheiros novos (mesmo em subpastas) são criados no servidor, criando as pastas em falta.
//!   2. pull: aplica o feed. Coloca cada ficheiro sob o caminho da sua pasta (espelho da árvore);
//!      se só a pasta mudou (mesmo hash), MOVE o ficheiro local em vez de rebaixar; tombstone apaga
//!      só o que este daemon baixou. O cursor só avança por página aplicada.
//! Segurança: caminhos sanitizados (anti-traversal, incl. subpastas), escrita atómica, e o delete
//! LOCAL não é propagado ao servidor.

use std::collections::HashSet;
use std::fs;
use std::path::{Path, PathBuf};
use std::sync::Mutex;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use anyhow::Result;
use sha2::{Digest, Sha256};

use crate::api::{Client, FileChange, PutOutcome};
use crate::config::SyncPair;
use crate::index::Index;
use crate::safepath::{safe_join_rel, sanitize_name};
use crate::tree::Tree;

pub struct Stats {
    pub downloaded: usize,
    pub deleted: usize,
    pub skipped: usize,
    pub uploaded: usize,
    pub conflicts: usize,
    pub moved: usize,
    /// Ficheiros apagados localmente que foram para a lixeira do servidor.
    pub trashed: usize,
    /// Falhas de envio/criação (cota, rede, servidor). Visível no estado para não parecer que
    /// "0 enviados" = "nada a fazer" quando na verdade o upload falhou.
    pub errors: usize,
}

/// Guarda de segurança: se MUITOS ficheiros rastreados desaparecerem de uma vez, é quase certo que a
/// pasta ficou indisponível (disco externo, cloud drive desmontado, backup a meio) e não que a pessoa
/// apagou tudo. Nesse caso NÃO propagamos apagar/mover — mais vale ficar dessincronizado do que
/// mandar a nuvem inteira para a lixeira.
const MAX_MISSING_TO_PROPAGATE: usize = 50;

/// mtime (epoch s) + tamanho de um ficheiro. (0,0) se não der para ler — nesse caso o pré-filtro
/// nunca acerta e caímos no caminho seguro (ler e hashear).
fn stat_of(path: &Path) -> (i64, i64) {
    match fs::metadata(path) {
        Ok(md) => {
            let mtime = md
                .modified()
                .ok()
                .and_then(|t| t.duration_since(UNIX_EPOCH).ok())
                .map(|d| d.as_secs() as i64)
                .unwrap_or(0);
            (mtime, md.len() as i64)
        }
        Err(_) => (0, 0),
    }
}

/// SHA-256 de um ficheiro, lido em blocos (não carrega o ficheiro inteiro em memória).
fn hash_file(path: &Path) -> Result<String> {
    use std::io::Read;
    let mut f = fs::File::open(path)?;
    let mut h = Sha256::new();
    let mut buf = [0u8; 64 * 1024];
    loop {
        let n = f.read(&mut buf)?;
        if n == 0 {
            break;
        }
        h.update(&buf[..n]);
    }
    let sum = h.finalize();
    let mut s = String::with_capacity(64);
    for b in sum {
        s.push_str(&format!("{b:02x}"));
    }
    Ok(s)
}

/// Abaixo deste tamanho não compensa o delta (duas viagens à rede para poupar pouco).
const DELTA_MIN_SIZE: u64 = 8 << 20; // 8 MiB
/// Tamanho de bloco pedido ao servidor.
const DELTA_BLOCK: u64 = 4 << 20; // 4 MiB

fn sha256_hex(bytes: &[u8]) -> String {
    let mut h = Sha256::new();
    h.update(bytes);
    let sum = h.finalize();
    let mut s = String::with_capacity(64);
    for b in sum {
        s.push_str(&format!("{b:02x}"));
    }
    s
}

/// Lê até encher `buf` (ou até ao fim do ficheiro). Devolve quantos bytes leu.
fn read_block(f: &mut fs::File, buf: &mut [u8]) -> std::io::Result<usize> {
    use std::io::Read;
    let mut got = 0;
    while got < buf.len() {
        match f.read(&mut buf[got..])? {
            0 => break,
            n => got += n,
        }
    }
    Ok(got)
}

/// Tenta enviar só os blocos alterados (M4). Devolve `Ok(None)` quando não compensa ou não dá — o
/// chamador faz então o envio completo. Nunca falha a sincronização por causa do delta.
fn try_delta_upload(
    client: &Client,
    path: &Path,
    id: &str,
    base_hash: &str,
) -> Result<Option<PutOutcome>> {
    let size = match fs::metadata(path) {
        Ok(m) => m.len(),
        Err(_) => return Ok(None),
    };
    if size < DELTA_MIN_SIZE {
        return Ok(None); // ficheiro pequeno: enviar inteiro é mais simples e igualmente rápido
    }
    let info = match client.file_blocks(id, DELTA_BLOCK) {
        Ok(i) => i,
        Err(_) => return Ok(None), // servidor antigo/sem suporte: caminho normal
    };
    // O delta só é válido sobre a MESMA base; se o servidor já mudou, deixa o caminho normal
    // apanhar o 412 e tratar o conflito.
    if info.hash != base_hash || info.block_size == 0 {
        return Ok(None);
    }

    let bs = info.block_size as usize;
    let mut f = match fs::File::open(path) {
        Ok(f) => f,
        Err(_) => return Ok(None),
    };
    let mut buf = vec![0u8; bs];
    let mut changed: Vec<usize> = Vec::new();
    let mut body: Vec<u8> = Vec::new();
    let mut i = 0usize;
    loop {
        let n = match read_block(&mut f, &mut buf) {
            Ok(n) => n,
            Err(_) => return Ok(None),
        };
        if n == 0 {
            break;
        }
        // Bloco difere do que o servidor tem (ou é novo, para lá do fim do ficheiro remoto).
        if info.hashes.get(i).map(String::as_str) != Some(sha256_hex(&buf[..n]).as_str()) {
            changed.push(i);
            body.extend_from_slice(&buf[..n]);
        }
        i += 1;
        if n < bs {
            break; // último bloco (parcial)
        }
    }

    // Se mudou quase tudo, o delta só acrescenta uma viagem extra — envia inteiro.
    if body.len() as u64 * 10 >= size * 7 {
        return Ok(None);
    }
    Ok(Some(client.update_file_delta(
        id,
        base_hash,
        info.block_size,
        size,
        &changed,
        body,
    )?))
}

/// De quanto em quanto tempo se faz a reconciliação completa (varredura do feed desde o início).
const RECONCILE_EVERY: Duration = Duration::from_secs(3600);
const RECONCILE_KEY: &str = "last_reconcile";

/// Reconciliação por varredura — resolve o furo do feed incremental: quando alguém **esvazia a
/// lixeira** no servidor, a linha desaparece (hard delete) e o feed (que é baseado em `updated_at`)
/// nunca emite tombstone. Sem isto o ficheiro ficaria órfão localmente para sempre.
///
/// Varre o feed DESDE O INÍCIO (sem tocar no cursor incremental), junta os ids que o servidor ainda
/// conhece, e remove localmente o que já não existe lá.

/// Atalho para incrementar um contador do `Stats` partilhado sem espalhar locks pelo código.
fn bump(stats: &Mutex<Stats>, f: impl FnOnce(&mut Stats)) {
    f(&mut stats.lock().unwrap_or_else(|e| e.into_inner()));
}

/// Corre uma passagem completa: primeiro sobe o local (push), depois aplica o feed (pull).
/// `workers` = transferências em paralelo (o índice é serializado por mutex; a rede é que paraleliza).
pub fn run_once(
    client: &Client,
    index: &Index,
    pairs: &[SyncPair],
    workers: usize,
) -> Result<Stats> {
    for p in pairs {
        fs::create_dir_all(&p.local_path)?;
    }
    let stats = Mutex::new(Stats {
        downloaded: 0,
        deleted: 0,
        skipped: 0,
        uploaded: 0,
        conflicts: 0,
        moved: 0,
        trashed: 0,
        errors: 0,
    });

    // Árvore de pastas do servidor (para colocar/criar ficheiros no sítio certo).
    let mut tree = Tree::new(client.folders()?);

    for pair in pairs {
        push_pair(client, index, pair, &mut tree, workers, &stats)?;
    }

    loop {
        let cursor = index.cursor()?;
        let page = client.changes(&cursor, 500)?;
        if page.changes.is_empty() {
            break;
        }
        // Os ficheiros de uma página são independentes entre si (ids distintos) — aplica-os em
        // paralelo. Uma falha isolada não aborta a passagem: fica para a próxima (o cursor só
        // avança no fim da página, por isso nada se perde).
        crate::parallel::for_each(page.changes.clone(), workers, |ch| {
            if let Err(e) = apply_one(client, index, pairs, &tree, &ch, &stats) {
                eprintln!("aviso: falhou aplicar {}: {e:#}", ch.name);
            }
        });
        // Só agora avançamos o cursor: se algo acima falhar, retomamos a mesma página (idempotente).
        index.set_cursor(&page.cursor)?;
        if !page.has_more {
            break;
        }
    }

    // Reconciliação periódica (apanha purgas que o feed incremental não consegue reportar).
    let now = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_secs())
        .unwrap_or(0);
    let last: u64 = index
        .meta_get(RECONCILE_KEY)
        .and_then(|v| v.parse().ok())
        .unwrap_or(0);
    if now.saturating_sub(last) >= RECONCILE_EVERY.as_secs() {
        reconcile(client, index, pairs, &tree, &stats)?;
        index.meta_set(RECONCILE_KEY, &now.to_string())?;
    }

    Ok(stats.into_inner().unwrap_or_else(|e| e.into_inner()))
}

/// Caminho relativo (com '/') onde um ficheiro do feed deve ficar: <pasta>/<nome>. Cai na raiz se a
/// pasta não resolver. Devolve None se o próprio nome do ficheiro for inseguro.
///
/// Os partilhados comigo vão para `Partilhados/<nome>`: a `folder_id` deles pertence à árvore do
/// DONO, que eu não consigo (nem devo) resolver — por isso ficam planos na pasta reservada.

/// Sobe as mudanças locais: ficheiros rastreados cujo conteúdo mudou (PUT com If-Match) e ficheiros
/// novos em qualquer subpasta (POST, criando as pastas do servidor em falta). Delete local NÃO é
/// propagado (segurança contra apagar dados por engano).

/// Junta os caminhos relativos (com '/') de todos os ficheiros sob `dir`, recursivamente.
/// Lixo do sistema operativo e temporários de edição: nunca sobem para o Drive. São ruído para o
/// utilizador (o `.DS_Store` do macOS aparecia na lista de ficheiros) e mudam sozinhos o tempo todo,
/// gerando sincronizações inúteis.
fn is_ignored(name: &str) -> bool {
    const EXACT: &[&str] = &[
        ".DS_Store",
        "Thumbs.db",
        "desktop.ini",
        ".localized",
        "Icon\r",
    ];
    if EXACT.iter().any(|n| n.eq_ignore_ascii_case(name)) {
        return true;
    }
    // Temporários de edição (Office `~$x.docx`, vim `x.swp`, guardados parciais nossos).
    name.starts_with("~$")
        || name.starts_with(".~lock.")
        || name.ends_with(".swp")
        || name.ends_with(".part")
        || name.ends_with(".crdownload")
        || name.ends_with(".mailin-part")
}

fn collect_files(root: &Path, dir: &Path, out: &mut Vec<String>) -> Result<()> {
    for entry in fs::read_dir(dir)? {
        let entry = entry?;
        let path = entry.path();
        let name = entry.file_name().to_string_lossy().to_string();
        if is_ignored(&name) {
            continue;
        }
        if path.is_dir() {
            collect_files(root, &path, out)?;
        } else if path.is_file() {
            if let Ok(rel) = path.strip_prefix(root) {
                // Normaliza para '/' (o índice e o servidor usam '/').
                let rel = rel
                    .to_string_lossy()
                    .replace(std::path::MAIN_SEPARATOR, "/");
                out.push(rel);
            }
        }
    }
    Ok(())
}

/// Copia o ficheiro para `<nome> (conflito <epoch>).<ext>` — preserva a versão local sem a perder.
fn save_conflict_copy(src: &Path) -> Result<()> {
    let secs = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_secs())
        .unwrap_or(0);
    let p = src;
    let stem = p.file_stem().and_then(|s| s.to_str()).unwrap_or("ficheiro");
    let cname = match p.extension().and_then(|e| e.to_str()) {
        Some(ext) => format!("{stem} (conflito {secs}).{ext}"),
        None => format!("{stem} (conflito {secs})"),
    };
    let dest = src.with_file_name(cname);
    fs::copy(&src, &dest)?;
    Ok(())
}

/// Onde é que uma mudança do feed deve aterrar no disco — ou `None` se o ficheiro **não pertence a
/// nenhum par sincronizado** (é o que faz com que só as pastas escolhidas sincronizem, em vez do
/// drive inteiro). Devolve também o par, para se saber a que pasta local pertence.
fn dest_for<'a>(
    tree: &Tree,
    pairs: &'a [SyncPair],
    ch: &FileChange,
) -> Option<(&'a SyncPair, PathBuf)> {
    let name = sanitize_name(&ch.name)?;
    for pair in pairs {
        if let Some(rel_dir) = tree.rel_path_under(ch.folder_id.as_deref(), &pair.folder_id) {
            let rel = if rel_dir.is_empty() {
                name.clone()
            } else {
                format!("{rel_dir}/{name}")
            };
            let dest = safe_join_rel(Path::new(&pair.local_path), &rel).ok()?;
            return Some((pair, dest));
        }
    }
    None
}

/// Aplica uma mudança vinda do servidor (pull), se ela cair dentro de algum par.
fn apply_one(
    client: &Client,
    index: &Index,
    pairs: &[SyncPair],
    tree: &Tree,
    ch: &FileChange,
    stats: &Mutex<Stats>,
) -> Result<()> {
    if ch.deleted {
        // Tombstone: só apaga o que ESTE agente baixou (está no índice). Nunca toca noutros ficheiros.
        if let Some(entry) = index.get(&ch.id)? {
            let path = Path::new(&entry.rel_path);
            if path.exists() {
                let _ = fs::remove_file(path);
                bump(stats, |s| s.deleted += 1);
            }
            index.remove(&ch.id)?;
        }
        return Ok(());
    }

    let (_pair, dest) = match dest_for(tree, pairs, ch) {
        Some(d) => d,
        None => return Ok(()), // fora das pastas sincronizadas: ignora
    };
    let dest_str = dest.to_string_lossy().to_string();

    if let Some(entry) = index.get(&ch.id)? {
        if entry.hash == ch.hash {
            if entry.rel_path == dest_str {
                bump(stats, |s| s.skipped += 1);
                return Ok(());
            }
            // Mesmo conteúdo noutro caminho ⇒ movido/renomeado no servidor: move o ficheiro local.
            let from = Path::new(&entry.rel_path);
            if from.exists() {
                if let Some(parent) = dest.parent() {
                    fs::create_dir_all(parent)?;
                }
                if fs::rename(from, &dest).is_err() {
                    fs::copy(from, &dest)?;
                    let _ = fs::remove_file(from);
                }
            }
            let (mt, sz) = stat_of(&dest);
            index.upsert(&ch.id, &dest_str, &ch.hash, ch.version, mt, sz)?;
            bump(stats, |s| s.moved += 1);
            return Ok(());
        }
    }

    let bytes = client.download(&ch.id)?;
    if let Some(parent) = dest.parent() {
        fs::create_dir_all(parent)?;
    }
    let tmp = dest.with_extension("mailin-part"); // escrita atómica
    fs::write(&tmp, &bytes)?;
    fs::rename(&tmp, &dest)?;

    let (mt, sz) = stat_of(&dest);
    index.upsert(&ch.id, &dest_str, &ch.hash, ch.version, mt, sz)?;
    bump(stats, |s| s.downloaded += 1);
    Ok(())
}

/// Sobe as mudanças de UM par: conteúdo alterado, ficheiros novos (criando as subpastas na nuvem
/// DENTRO da pasta do par), moves e apagados (→ lixeira).
fn push_pair(
    client: &Client,
    index: &Index,
    pair: &SyncPair,
    tree: &mut Tree,
    workers: usize,
    stats: &Mutex<Stats>,
) -> Result<()> {
    let root = Path::new(&pair.local_path);
    // Só as entradas do índice que pertencem a ESTA pasta local.
    let tracked: Vec<_> = index
        .all()?
        .into_iter()
        .filter(|t| Path::new(&t.rel_path).starts_with(root))
        .collect();
    let tracked_paths: HashSet<String> = tracked.iter().map(|t| t.rel_path.clone()).collect();

    let mut found_rel = Vec::new();
    collect_files(root, root, &mut found_rel)?;
    let found_abs: HashSet<String> = found_rel
        .iter()
        .map(|r| root.join(r).to_string_lossy().to_string())
        .collect();

    // 1) Alterados / desaparecidos.
    let mut missing: Vec<&crate::index::Tracked> = Vec::new();
    let mut to_upload: Vec<(String, String, String)> = Vec::new(); // (id, caminho abs, hash base)
    for t in &tracked {
        if !found_abs.contains(&t.rel_path) {
            missing.push(t);
            continue;
        }
        let path = Path::new(&t.rel_path);
        let (mtime, size) = stat_of(path);
        if mtime == t.mtime && size == t.size {
            continue; // pré-filtro: nada mudou
        }
        let h = match hash_file(path) {
            Ok(h) => h,
            Err(_) => continue,
        };
        if h == t.hash {
            index.touch_stat(&t.file_id, mtime, size)?;
            continue;
        }
        to_upload.push((t.file_id.clone(), t.rel_path.clone(), t.hash.clone()));
    }

    crate::parallel::for_each(to_upload, workers, |(id, abs, base_hash)| {
        let path = PathBuf::from(&abs);
        let outcome = match try_delta_upload(client, &path, &id, &base_hash) {
            Ok(Some(o)) => Ok(o),
            _ => match fs::read(&path) {
                Ok(bytes) => client.update_file(&id, &base_hash, &bytes),
                Err(_) => return,
            },
        };
        match outcome {
            Ok(PutOutcome::Updated(m)) => {
                let (mt, sz) = stat_of(&path);
                let _ = index.upsert(&id, &abs, &m.hash, m.versao, mt, sz);
                bump(stats, |s| s.uploaded += 1);
            }
            Ok(PutOutcome::Conflict) => {
                let _ = save_conflict_copy(&path);
                bump(stats, |s| s.conflicts += 1);
            }
            Err(e) => {
                eprintln!("aviso: envio de {abs} falhou: {e:#}");
                bump(stats, |s| s.errors += 1);
            }
        }
    });

    // 2) Novos locais (ainda não rastreados).
    let mut new_rel: Vec<String> = found_rel
        .into_iter()
        .filter(|r| {
            let abs = root.join(r).to_string_lossy().to_string();
            !tracked_paths.contains(&abs) && !r.ends_with(".mailin-part")
        })
        .collect();

    // 3) Os que sumiram: movidos (mesmo hash noutro sítio) ou apagados (→ lixeira).
    if missing.len() > MAX_MISSING_TO_PROPAGATE {
        eprintln!(
            "aviso: {} ficheiros em falta em {} — apagar/mover NÃO foi propagado (pasta indisponível?)",
            missing.len(),
            pair.name
        );
    } else if !missing.is_empty() {
        let new_hashes: Vec<(String, String)> = new_rel
            .iter()
            .filter_map(|r| hash_file(&root.join(r)).ok().map(|h| (r.clone(), h)))
            .collect();
        let mut consumed: HashSet<String> = HashSet::new();
        for t in missing {
            let matched = new_hashes
                .iter()
                .find(|(rel, h)| *h == t.hash && !consumed.contains(rel))
                .map(|(rel, _)| rel.clone());
            match matched {
                Some(rel) => {
                    let mut segs: Vec<String> = rel.split('/').map(String::from).collect();
                    let name = segs.pop().unwrap_or_default();
                    if name.is_empty() {
                        continue;
                    }
                    let folder_id = tree.resolve_or_create_under(client, &pair.folder_id, &segs)?;
                    let m = client.move_file(&t.file_id, Some(&folder_id), &name)?;
                    let abs = root.join(&rel).to_string_lossy().to_string();
                    let (mt, sz) = stat_of(Path::new(&abs));
                    index.upsert(&t.file_id, &abs, &m.hash, m.versao, mt, sz)?;
                    consumed.insert(rel);
                    bump(stats, |s| s.moved += 1);
                }
                None => {
                    client.trash_file(&t.file_id)?;
                    index.remove(&t.file_id)?;
                    bump(stats, |s| s.trashed += 1);
                }
            }
        }
        new_rel.retain(|r| !consumed.contains(r));
    }

    // 4) Criar os novos na nuvem, DENTRO da pasta do par.
    let mut to_create: Vec<(String, String, String)> = Vec::new(); // (rel, nome, pasta destino)
    for rel in new_rel {
        let mut segs: Vec<String> = rel.split('/').map(String::from).collect();
        let name = segs.pop().unwrap_or_default();
        if name.is_empty() {
            continue;
        }
        let folder_id = tree.resolve_or_create_under(client, &pair.folder_id, &segs)?;
        to_create.push((rel, name, folder_id));
    }
    crate::parallel::for_each(to_create, workers, |(rel, name, folder_id)| {
        let path = root.join(&rel);
        let bytes = match fs::read(&path) {
            Ok(b) => b,
            Err(_) => return,
        };
        match client.create_file(&name, Some(&folder_id), &bytes) {
            Ok(m) => {
                let abs = path.to_string_lossy().to_string();
                let (mt, sz) = stat_of(&path);
                let _ = index.upsert(&m.id, &abs, &m.hash, m.versao, mt, sz);
                bump(stats, |s| s.uploaded += 1);
            }
            Err(e) => {
                eprintln!("aviso: criar {rel} falhou: {e:#}");
                bump(stats, |s| s.errors += 1);
            }
        }
    });
    Ok(())
}

/// Reconciliação: apanha purgas (hard delete no servidor, que o feed não reporta) e ficheiros que
/// saíram das pastas sincronizadas. Guardas iguais às de antes.
fn reconcile(
    client: &Client,
    index: &Index,
    pairs: &[SyncPair],
    tree: &Tree,
    stats: &Mutex<Stats>,
) -> Result<()> {
    let mut alive: HashSet<String> = HashSet::new();
    let mut cursor = String::new();
    loop {
        let page = client.changes(&cursor, 500)?;
        if page.changes.is_empty() {
            break;
        }
        for ch in &page.changes {
            // Só conta como "vivo" o que ainda pertence a um par (e não está na lixeira).
            if !ch.deleted && dest_for(tree, pairs, ch).is_some() {
                alive.insert(ch.id.clone());
            }
        }
        cursor = page.cursor.clone();
        if !page.has_more {
            break;
        }
    }

    let tracked = index.all()?;
    if alive.is_empty() && !tracked.is_empty() {
        eprintln!("aviso: reconciliação abortada — o servidor não devolveu ficheiros");
        return Ok(());
    }
    let orphans: Vec<_> = tracked
        .into_iter()
        .filter(|t| !alive.contains(&t.file_id))
        .collect();
    if orphans.len() > MAX_MISSING_TO_PROPAGATE {
        eprintln!(
            "aviso: reconciliação abortada — {} órfãos (demasiados para apagar sem confirmação)",
            orphans.len()
        );
        return Ok(());
    }
    for t in orphans {
        let p = Path::new(&t.rel_path);
        if p.exists() {
            let _ = fs::remove_file(p);
            bump(stats, |s| s.deleted += 1);
        }
        index.remove(&t.file_id)?;
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::api::Folder;

    fn folder(id: &str, parent: Option<&str>, name: &str) -> Folder {
        Folder {
            id: id.into(),
            parent_id: parent.map(|s| s.into()),
            name: name.into(),
        }
    }

    #[test]
    fn os_junk_and_edit_temporaries_never_sync() {
        for junk in [
            ".DS_Store",
            "Thumbs.db",
            "desktop.ini",
            "~$relatorio.docx",
            ".~lock.contas.ods#",
            "notas.txt.swp",
            "video.mp4.part",
            "x.mailin-part",
        ] {
            assert!(is_ignored(junk), "devia ser ignorado: {junk}");
        }
        // Ficheiros legítimos com nomes parecidos continuam a sincronizar.
        for ok in [
            "relatorio.docx",
            "notas.txt",
            ".env",
            "DS_Store.txt",
            "a~$b.txt",
        ] {
            assert!(!is_ignored(ok), "NÃO devia ser ignorado: {ok}");
        }
    }

    fn change(id: &str, folder_id: Option<&str>, name: &str) -> FileChange {
        FileChange {
            id: id.into(),
            folder_id: folder_id.map(|s| s.into()),
            name: name.into(),
            hash: "h".into(),
            size: 1,
            version: 1,
            deleted: false,
            updated_at: String::new(),
            owned: true,
        }
    }

    fn pair(local: &str, folder_id: &str) -> SyncPair {
        SyncPair {
            local_path: local.into(),
            folder_id: folder_id.into(),
            name: "Trabalho".into(),
        }
    }

    /// O ponto central do modelo: **só** o que está dentro de uma pasta sincronizada aterra no
    /// disco. Um ficheiro noutro sítio do Drive (ou na raiz) é ignorado.
    #[test]
    fn only_files_inside_a_pair_are_synced() {
        let tree = Tree::new(vec![
            folder("work", None, "Trabalho"),
            folder("sub", Some("work"), "2024"),
            folder("other", None, "Pessoal"),
        ]);
        let pairs = vec![pair("/local/Trabalho", "work")];

        // Na própria pasta do par → vai para a raiz da pasta local.
        let (_, dest) = dest_for(&tree, &pairs, &change("1", Some("work"), "a.txt")).unwrap();
        assert_eq!(dest, Path::new("/local/Trabalho/a.txt"));

        // Numa subpasta → mantém a subárvore.
        let (_, dest) = dest_for(&tree, &pairs, &change("2", Some("sub"), "b.txt")).unwrap();
        assert_eq!(dest, Path::new("/local/Trabalho/2024/b.txt"));

        // Fora do par (outra pasta de topo) → NÃO sincroniza.
        assert!(dest_for(&tree, &pairs, &change("3", Some("other"), "c.txt")).is_none());
        // Na raiz do Drive → também não (não pertence a nenhum par).
        assert!(dest_for(&tree, &pairs, &change("4", None, "d.txt")).is_none());
    }

    #[test]
    fn several_pairs_are_routed_to_their_own_folder() {
        let tree = Tree::new(vec![
            folder("w", None, "Trabalho"),
            folder("p", None, "Fotos"),
        ]);
        let pairs = vec![pair("/local/Trabalho", "w"), pair("/local/Fotos", "p")];

        let (_, d1) = dest_for(&tree, &pairs, &change("1", Some("w"), "a.txt")).unwrap();
        let (_, d2) = dest_for(&tree, &pairs, &change("2", Some("p"), "b.jpg")).unwrap();
        assert_eq!(d1, Path::new("/local/Trabalho/a.txt"));
        assert_eq!(d2, Path::new("/local/Fotos/b.jpg"));
    }

    /// Um nome vindo do servidor nunca pode escapar da pasta do par.
    #[test]
    fn traversal_in_name_cannot_escape_the_pair() {
        let tree = Tree::new(vec![folder("w", None, "Trabalho")]);
        let pairs = vec![pair("/local/Trabalho", "w")];
        let (_, dest) =
            dest_for(&tree, &pairs, &change("1", Some("w"), "../../etc/passwd")).unwrap();
        assert!(dest.starts_with("/local/Trabalho"), "escapou: {dest:?}");
    }
}
