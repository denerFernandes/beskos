import { mediaUrl } from "../apiBase";

// Registo global dos emojis customizados do workspace (:nome: → URL da imagem). É preenchido uma vez
// quando o chat carrega a lista; o renderRich consulta-o para trocar :nome: por <img>. Global (e não
// prop) para não ter de passar o mapa por todos os pontos que renderizam texto (chat, comentários).

let MAP: Record<string, string> = {};

export function setCustomEmoji(list: { name: string; file_id: string }[]) {
  MAP = Object.fromEntries(list.map((e) => [e.name, mediaUrl(`/api/files/${e.file_id}/thumb`)]));
}

export function customEmojiUrl(name: string): string | undefined {
  return MAP[name];
}

export function customEmojiList(): { name: string; url: string }[] {
  return Object.entries(MAP).map(([name, url]) => ({ name, url }));
}
