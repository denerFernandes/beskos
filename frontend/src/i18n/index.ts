// i18n setup (law §4). Default pt-BR, en mandatory. Detects the browser's language.
import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import LanguageDetector from "i18next-browser-languagedetector";

import ptBR from "./locales/pt-BR/common.json";
import en from "./locales/en/common.json";

i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources: {
      "pt-BR": { common: ptBR },
      en: { common: en },
    },
    fallbackLng: "pt-BR",
    defaultNS: "common",
    interpolation: { escapeValue: false }, // React already escapes
  });

export default i18n;
