import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, type Language, type Me } from "../api";

export const LANGUAGES: { value: Language; labelKey: string }[] = [
  { value: "", labelKey: "settings.language.auto" },
  { value: "pt-BR", labelKey: "settings.language.ptBR" },
  { value: "en", labelKey: "settings.language.en" },
];

// Regra (lei §4 + pedido do user): a preferência SALVA na conta tem prioridade sobre o browser.
// Quando vazia, o i18next-browser-languagedetector cuida da detecção automática.
// setLanguage persiste na conta E aplica na hora.
export function useUserLanguage(me: Me | undefined) {
  const { i18n } = useTranslation();
  const qc = useQueryClient();

  // Aplica a preferência salva assim que o /me chega (browser é só o default inicial).
  useEffect(() => {
    if (me?.language && i18n.language !== me.language) {
      void i18n.changeLanguage(me.language);
    }
  }, [me?.language, i18n]);

  const mutation = useMutation({
    mutationFn: (lang: Language) => api.updateLanguage(lang),
    onSuccess: (_data, lang) => {
      // "" volta a ser automático: deixa o detector decidir na próxima carga.
      if (lang) void i18n.changeLanguage(lang);
      void qc.invalidateQueries({ queryKey: ["me"] });
    },
  });

  return { current: (me?.language ?? "") as Language, setLanguage: mutation.mutate, saving: mutation.isPending };
}
