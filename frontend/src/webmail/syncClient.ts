// Ponte da UI para o motor de sincronização que corre DENTRO da app desktop (Tauri).
//
// Modelo: pares de pastas. O utilizador escolhe uma pasta LOCAL; ela é criada no Drive e só o que
// está lá dentro sincroniza, nos dois sentidos. Podem existir várias. Nada de espelhar o drive todo.
// Na web estes comandos não existem — a UI mostra uma nota em vez dos controlos.

import { isTauri } from "./tauriNotify";
import { devicesApi } from "./backend";
import { API_BASE } from "../apiBase";

export type SyncPhase = "idle" | "syncing" | "error" | "paused";

/** Uma pasta local espelhada com uma pasta do Drive. */
export interface SyncPair {
  local_path: string;
  folder_id: string;
  name: string;
}

export interface SyncStatus {
  phase: SyncPhase;
  message: string;
  paused: boolean;
  /** false = esta máquina ainda não está ligada A ESTA CONTA (pode estar ligada a outra). */
  configured: boolean;
  /** Servidor/conta a que a config gravada pertence (para isolar por conta na mesma máquina). */
  server_url: string;
  account: string;
  pairs: SyncPair[];
}

// URL do servidor da sessão atual (igual ao usado no connect).
const serverUrl = (): string => API_BASE || window.location.origin;

export const syncSupported = (): boolean => isTauri();

// O invoke só existe no app nativo; import dinâmico para a web não carregar o módulo.
async function invoke<T>(cmd: string, args?: Record<string, unknown>): Promise<T> {
  const { invoke } = await import("@tauri-apps/api/core");
  return invoke<T>(cmd, args);
}

export const syncApi = {
  /** `account` = identidade da conta atual (e-mail do utilizador). O daemon esconde os pares se a
   *  config gravada for de outra conta (troca de utilizador na mesma máquina). */
  status: (account: string) => invoke<SyncStatus>("sync_status", { account, serverUrl: serverUrl() }),
  now: () => invoke<void>("sync_now"),
  setPaused: (paused: boolean) => invoke<void>("sync_set_paused", { paused }),
  bandwidth: () => invoke<number>("sync_bandwidth"),
  setBandwidth: (bandwidthKbps: number) => invoke<void>("sync_set_bandwidth", { bandwidthKbps }),
  removeFolder: (localPath: string) => invoke<void>("sync_remove_folder", { localPath }),

  /** Abre o seletor NATIVO de pasta; devolve o caminho escolhido (ou null se cancelar). */
  async pickFolder(): Promise<string | null> {
    const { open } = await import("@tauri-apps/plugin-dialog");
    const picked = await open({ directory: true, multiple: false });
    return typeof picked === "string" ? picked : null;
  },

  /**
   * Liga esta máquina à conta, se ainda não estiver: cria um token de dispositivo com a sessão
   * atual (o utilizador não copia nada à mão).
   */
  async ensureConnected(account: string): Promise<void> {
    const st = await syncApi.status(account);
    if (st.configured) return; // já ligada A ESTA conta (status esconde a config de outra conta)
    const name = `Beskos Desktop (${navigator.platform || "computador"})`;
    const created = await devicesApi.create(name);
    await invoke<void>("sync_connect", { serverUrl: serverUrl(), token: created.token, account });
  },

  /**
   * Escolhe uma pasta local e passa a sincronizá-la: é criada a pasta correspondente no Drive.
   * Devolve o par criado, ou null se o utilizador cancelou o seletor.
   */
  async addFolder(account: string): Promise<SyncPair | null> {
    const dir = await syncApi.pickFolder();
    if (!dir) return null;
    await syncApi.ensureConnected(account);
    return invoke<SyncPair>("sync_add_folder", { localPath: dir });
  },
};
