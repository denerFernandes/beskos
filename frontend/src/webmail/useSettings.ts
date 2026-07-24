import { useQuery, useQueryClient } from "@tanstack/react-query";
import { settingsApi, type UserSettings } from "./backend";
import { toast } from "./toast";

// Preferências do utilizador, partilhadas entre a página de Definições (escreve) e as apps (leem).
// Tudo passa pela mesma query ["settings"], então uma mudança nas Definições reflete-se logo nas
// apps abertas (cache partilhado). O servidor faz merge por secção; aqui espelhamos isso otimista.

const KEY = ["settings"] as const;

export function useUserSettings(): UserSettings {
  const q = useQuery({ queryKey: KEY, queryFn: () => settingsApi.get(), staleTime: 60_000, retry: false });
  return q.data ?? {};
}

// useSection dá a uma secção de Definições o seu valor + um `save` que grava só essa secção (merge
// otimista na cache; persiste no servidor; reverte com toast em erro).
export function useSection<K extends keyof UserSettings>(app: K) {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: KEY, queryFn: () => settingsApi.get(), staleTime: 60_000, retry: false });
  const value = (q.data?.[app] ?? {}) as NonNullable<UserSettings[K]>;

  const save = (patch: Partial<NonNullable<UserSettings[K]>>) => {
    const prev = qc.getQueryData<UserSettings>(KEY);
    const nextSection = { ...(prev?.[app] ?? {}), ...patch };
    qc.setQueryData<UserSettings>(KEY, { ...(prev ?? {}), [app]: nextSection });
    settingsApi.patch({ [app]: nextSection } as Partial<UserSettings>).catch((e) => {
      qc.setQueryData<UserSettings>(KEY, prev); // reverte
      toast((e as Error).message);
    });
  };

  return { value, save, loading: q.isLoading };
}
