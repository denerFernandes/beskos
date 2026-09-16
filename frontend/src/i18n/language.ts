import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, type Language, type Me } from "../api";

export const LANGUAGES: { value: Language; labelKey: string }[] = [
  { value: "", labelKey: "settings.language.auto" },
  { value: "pt-BR", labelKey: "settings.language.ptBR" },
  { value: "en", labelKey: "settings.language.en" },
];

// Rule (law §4 + user's request): the SAVED preference in the account takes priority over the browser.
// When empty, i18next-browser-languagedetector handles automatic detection.
// setLanguage persists to the account AND applies immediately.
export function useUserLanguage(me: Me | undefined) {
  const { i18n } = useTranslation();
  const qc = useQueryClient();

  // Apply the saved preference as soon as /me arrives (the browser is just the initial default).
  useEffect(() => {
    if (me?.language && i18n.language !== me.language) {
      void i18n.changeLanguage(me.language);
    }
  }, [me?.language, i18n]);

  const mutation = useMutation({
    mutationFn: (lang: Language) => api.updateLanguage(lang),
    onSuccess: (_data, lang) => {
      // "" goes back to automatic: let the detector decide on the next load.
      if (lang) void i18n.changeLanguage(lang);
      void qc.invalidateQueries({ queryKey: ["me"] });
    },
  });

  return { current: (me?.language ?? "") as Language, setLanguage: mutation.mutate, saving: mutation.isPending };
}
