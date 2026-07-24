import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { backend, gifsApi, type Gif } from "./backend";
import { Spinner } from "./Spinner";
import { toast } from "./toast";
import { IcTrash, IcUpload } from "../panel/icons";

// Biblioteca de GIFs pessoal. Sem Giphy/Tenor: o GIF é um ficheiro do Drive, e a busca de ninguém
// sai para um terceiro (decisão de 2026-07-16). Carrega uma vez, reusa sempre.
export function GifPicker({ anchor, onPick, onClose }: {
  anchor: DOMRect | null; onPick: (g: Gif) => void; onClose: () => void;
}) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const ref = useRef<HTMLDivElement>(null);
  const fileIn = useRef<HTMLInputElement>(null);
  const [busy, setBusy] = useState(false);
  const q = useQuery({ queryKey: ["gifs"], queryFn: () => gifsApi.list(), retry: false });

  // Igual ao seletor de emoji: o listener entra no tick seguinte, senão apanha o clique que abriu.
  useEffect(() => {
    const onDown = (e: MouseEvent) => { if (!ref.current?.contains(e.target as Node)) onClose(); };
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") onClose(); };
    const id = setTimeout(() => document.addEventListener("mousedown", onDown), 0);
    document.addEventListener("keydown", onKey);
    return () => { clearTimeout(id); document.removeEventListener("mousedown", onDown); document.removeEventListener("keydown", onKey); };
  }, [onClose]);

  const del = useMutation({
    mutationFn: (id: string) => gifsApi.remove(id),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["gifs"] }); toast(t("chat.gifRemoved")); },
    onError: (e) => toast((e as Error).message),
  });

  // Carregar: vai para o Drive e ganha a marca de GIF. Dois passos, um clique.
  const upload = async (files: FileList | null) => {
    const f = files?.[0];
    if (!f) return;
    if (!/gif|webp|png|jpe?g/i.test(f.type)) { toast(t("chat.gifOnlyImages")); return; }
    setBusy(true);
    try {
      const stored = await backend.uploadFile(f);
      await gifsApi.add(stored.id, f.name);
      await qc.invalidateQueries({ queryKey: ["gifs"] });
    } catch (e) {
      toast((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const style: React.CSSProperties = anchor
    ? { left: Math.max(8, Math.min(anchor.left - 100, window.innerWidth - 268)), top: Math.max(8, anchor.top - 288) }
    : {};
  const list = q.data ?? [];

  return (
    <div className="emoji-pop up gif-picker" ref={ref} style={style} onMouseDown={(e) => e.stopPropagation()}>
      <div className="gif-head">
        <b>{t("chat.gifLibrary")}</b>
        <button className="chipbtn" disabled={busy} onClick={() => fileIn.current?.click()}>
          {busy ? <Spinner size={12} /> : <IcUpload size={13} />} {t("chat.gifAdd")}
        </button>
        <input ref={fileIn} type="file" accept="image/gif,image/webp,image/png,image/jpeg" hidden
          onChange={(e) => { void upload(e.target.files); e.target.value = ""; }} />
      </div>
      {q.isLoading ? <div className="loading">…</div>
        : list.length === 0 ? <p className="emoji-none">{t("chat.gifEmpty")}</p>
        : (
          <div className="gif-grid">
            {list.map((g) => (
              <div className="gif-item" key={g.id}>
                <img src={backend.downloadFileUrl(g.file_id)} alt={g.name} loading="lazy"
                  onClick={() => onPick(g)} />
                <button className="ib gif-del" title={t("chat.gifRemove")}
                  onClick={(e) => { e.stopPropagation(); del.mutate(g.id); }}><IcTrash size={11} /></button>
              </div>
            ))}
          </div>
        )}
      <p className="gif-hint">{t("chat.gifHint")}</p>
    </div>
  );
}
