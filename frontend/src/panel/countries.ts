// Países para o formulário de faturação. Só os códigos: o NOME vem do Intl.DisplayNames no idioma
// de quem está a ver — traduzir 200 países à mão seria trabalho perdido e desatualizava.

/** Os 27 da União Europeia (decidem a regra do IVA). */
export const EU_COUNTRIES = [
  "AT", "BE", "BG", "HR", "CY", "CZ", "DK", "EE", "FI", "FR", "DE", "GR", "HU",
  "IE", "IT", "LV", "LT", "LU", "MT", "NL", "PL", "PT", "RO", "SK", "SI", "ES", "SE",
];

/** Fora da UE: os mercados onde já há clientes ou de onde chegam pedidos. */
const NON_EU = [
  "GB", "CH", "NO", "IS", "UA", "US", "CA", "BR", "MX", "AR", "CL", "CO", "AO", "MZ",
  "CV", "ZA", "AE", "IL", "TR", "IN", "AU", "NZ", "JP", "SG",
];

/** Lista completa do seletor: a UE primeiro (é onde a regra do IVA morde), depois o resto. */
export const COUNTRIES = [...EU_COUNTRIES, ...NON_EU];
