//! Segurança: nunca escrever fora da pasta de sincronização. O nome do ficheiro vem do servidor;
//! um servidor comprometido poderia enviar `../../.ssh/authorized_keys`. Aqui sanitizamos o nome
//! para um único componente seguro e resolvemos o caminho final DENTRO da raiz, rejeitando qualquer
//! coisa que escape.

use std::path::{Component, Path, PathBuf};

/// Sanitiza um nome de ficheiro para um único componente seguro. Devolve None se não sobrar nada
/// utilizável (o chamador trata como entrada inválida e ignora o ficheiro).
pub fn sanitize_name(name: &str) -> Option<String> {
    // Corta qualquer separador de caminho (o nome tem de ser UM componente, nunca uma subárvore).
    let base = name
        .rsplit(|c| c == '/' || c == '\\')
        .next()
        .unwrap_or("")
        .trim();
    // Rejeita vazios, "." e ".." e nomes com NUL (evita truncagem em chamadas de SO).
    if base.is_empty() || base == "." || base == ".." || base.contains('\0') {
        return None;
    }
    Some(base.to_string())
}

/// Resolve um caminho RELATIVO com subpastas (segmentos separados por '/') DENTRO de `root`. Cada
/// segmento é sanitizado; qualquer `..`/`.` inseguro faz erro e o resultado tem de ficar sob a raiz.
pub fn safe_join_rel(root: &Path, rel: &str) -> anyhow::Result<PathBuf> {
    let mut out = root.to_path_buf();
    for seg in rel.split('/') {
        if seg.is_empty() {
            continue;
        }
        let clean =
            sanitize_name(seg).ok_or_else(|| anyhow::anyhow!("segmento de caminho inválido"))?;
        out.push(clean);
    }
    // Verificação final: nada de componentes `..` e o resultado tem de ficar sob a raiz.
    for comp in out.components() {
        if matches!(comp, Component::ParentDir) {
            anyhow::bail!("caminho tenta subir de diretório");
        }
    }
    if !out.starts_with(root) {
        anyhow::bail!("caminho resolvido fora da raiz de sincronização");
    }
    Ok(out)
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::path::Path;

    #[test]
    fn rejects_traversal_names() {
        assert_eq!(sanitize_name("../secret"), Some("secret".to_string())); // separador cortado
        assert_eq!(sanitize_name(".."), None);
        assert_eq!(sanitize_name("."), None);
        assert_eq!(sanitize_name(""), None);
        assert_eq!(sanitize_name("a/b/c.txt"), Some("c.txt".to_string()));
        assert_eq!(sanitize_name("evil\0.txt"), None);
    }

    #[test]
    fn safe_join_rel_handles_subdirs() {
        let root = Path::new("/sync/root");
        assert_eq!(
            safe_join_rel(root, "docs/2024/plano.txt").unwrap(),
            Path::new("/sync/root/docs/2024/plano.txt")
        );
        // Barras iniciais/duplas são inofensivas (segmentos vazios ignorados).
        assert_eq!(
            safe_join_rel(root, "a//b.txt").unwrap(),
            Path::new("/sync/root/a/b.txt")
        );
        // Qualquer segmento ".." é rejeitado (nunca escapa da raiz).
        assert!(safe_join_rel(root, "docs/../etc/passwd").is_err());
        assert!(safe_join_rel(root, "../x").is_err());
    }
}
