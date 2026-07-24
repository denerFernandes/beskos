import type { NextConfig } from "next";

// Proxy da API do Manager: o browser fala sempre com a própria origem (/api/manager/*) e o Next
// reencaminha para o manager-api. Assim o cookie de sessão (HttpOnly) fica first-party.
const apiURL = process.env.MANAGER_API_URL ?? "http://localhost:8091";

const nextConfig: NextConfig = {
  // Imagem Docker enxuta: o Next empacota só o necessário para correr (node server standalone).
  output: "standalone",
  async rewrites() {
    return [{ source: "/api/manager/:path*", destination: `${apiURL}/api/manager/:path*` }];
  },
};

export default nextConfig;
