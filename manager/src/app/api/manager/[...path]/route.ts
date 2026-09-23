import { NextRequest } from "next/server";

// Proxy to manager-api. The browser always talks to its own origin (/api/manager/*), so the
// session cookie (HttpOnly) stays first-party — that is the reason this hop exists.
//
// Why a route handler and not a next.config `rewrites()`: rewrites are resolved at BUILD time and
// get written into the manifest. Since the image is built in CI, without the environment's
// MANAGER_API_URL, the default `localhost:8091` got baked in — and in production login returned
// Internal Server Error with ECONNREFUSED 127.0.0.1:8091 (2026-07-28). Here the variable is read on
// every request, on the server where the app runs.

export const dynamic = "force-dynamic"; // none of this may be prerendered or cached

function apiURL(): string {
  return (process.env.MANAGER_API_URL ?? "http://localhost:8091").replace(/\/$/, "");
}

async function forward(req: NextRequest, path: string[]): Promise<Response> {
  const url = `${apiURL()}/api/manager/${path.join("/")}${req.nextUrl.search}`;
  const headers = new Headers();
  // Only what the API needs: cookie (session), content type and the source IP for its logs.
  for (const h of ["cookie", "content-type", "accept", "user-agent", "x-forwarded-for"]) {
    const v = req.headers.get(h);
    if (v) headers.set(h, v);
  }
  if (!headers.has("x-forwarded-for")) {
    const ip = req.headers.get("x-real-ip");
    if (ip) headers.set("x-forwarded-for", ip);
  }

  const method = req.method;
  const body = method === "GET" || method === "HEAD" ? undefined : await req.arrayBuffer();
  let upstream: Response;
  try {
    upstream = await fetch(url, { method, headers, body, redirect: "manual", cache: "no-store" });
  } catch (e) {
    // Without this Next returned an opaque 500; this way the real cause lands in the container log.
    console.error(`manager proxy: ${method} ${url}: ${String(e)}`);
    return Response.json({ error_code: "manager.upstream_unreachable" }, { status: 502 });
  }

  const out = new Headers();
  const ct = upstream.headers.get("content-type");
  if (ct) out.set("content-type", ct);
  // Set-Cookie may repeat (session + flags); getSetCookie preserves each line.
  for (const c of upstream.headers.getSetCookie?.() ?? []) out.append("set-cookie", c);
  return new Response(upstream.body, { status: upstream.status, headers: out });
}

type Ctx = { params: Promise<{ path: string[] }> };
const handler = async (req: NextRequest, ctx: Ctx) => forward(req, (await ctx.params).path);

export { handler as GET, handler as POST, handler as PUT, handler as PATCH, handler as DELETE };
