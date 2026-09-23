import type { NextConfig } from "next";

// The Manager API proxy lives in src/app/api/manager/[...path]/route.ts, not here:
// `rewrites()` are resolved at BUILD time and the image is built in CI, without the
// environment's MANAGER_API_URL — localhost got baked in and login broke in production.
const nextConfig: NextConfig = {
  // Lean Docker image: Next packages only what it needs to run (standalone node server).
  output: "standalone",
};

export default nextConfig;
