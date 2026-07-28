import { NextRequest } from "next/server";

// Proxy para a manager-api. O browser fala sempre com a própria origem (/api/manager/*), por isso o
// cookie de sessão (HttpOnly) fica first-party — é essa a razão de existir este salto.
//
// Porque é um route handler e não um `rewrites()` do next.config: os rewrites são resolvidos em
// tempo de BUILD e ficam gravados no manifesto. Como a imagem é construída no CI, sem o
// MANAGER_API_URL do ambiente, ficava lá o default `localhost:8091` — e em produção o login dava
// Internal Server Error com ECONNREFUSED 127.0.0.1:8091 (28/07/2026). Aqui a variável é lida a cada
// pedido, no servidor onde a app corre.

export const dynamic = "force-dynamic"; // nada disto pode ser pré-renderizado nem cacheado

function apiURL(): string {
  return (process.env.MANAGER_API_URL ?? "http://localhost:8091").replace(/\/$/, "");
}

async function forward(req: NextRequest, path: string[]): Promise<Response> {
  const url = `${apiURL()}/api/manager/${path.join("/")}${req.nextUrl.search}`;
  const headers = new Headers();
  // Só o que a API precisa: cookie (sessão), tipo de conteúdo e o IP de origem para os logs dela.
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
    // Sem isto o Next devolvia um 500 opaco; assim o motivo real fica no log do contentor.
    console.error(`manager proxy: ${method} ${url}: ${String(e)}`);
    return Response.json({ error_code: "manager.upstream_unreachable" }, { status: 502 });
  }

  const out = new Headers();
  const ct = upstream.headers.get("content-type");
  if (ct) out.set("content-type", ct);
  // Set-Cookie pode vir repetido (sessão + flags); getSetCookie preserva cada linha.
  for (const c of upstream.headers.getSetCookie?.() ?? []) out.append("set-cookie", c);
  return new Response(upstream.body, { status: upstream.status, headers: out });
}

type Ctx = { params: Promise<{ path: string[] }> };
const handler = async (req: NextRequest, ctx: Ctx) => forward(req, (await ctx.params).path);

export { handler as GET, handler as POST, handler as PUT, handler as PATCH, handler as DELETE };
