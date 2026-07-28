import type { NextConfig } from "next";

// O proxy da API do Manager vive em src/app/api/manager/[...path]/route.ts, e não aqui: os
// `rewrites()` são resolvidos em tempo de BUILD e a imagem é construída no CI, sem o
// MANAGER_API_URL do ambiente — ficava lá o localhost e o login rebentava em produção.
const nextConfig: NextConfig = {
  // Imagem Docker enxuta: o Next empacota só o necessário para correr (node server standalone).
  output: "standalone",
};

export default nextConfig;
