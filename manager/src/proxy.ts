import { NextResponse } from "next/server";
import type { NextRequest } from "next/server";

// Gate rápido por cookie: sem sessão → /login; com sessão em /login → home. A validação REAL da
// sessão é do manager-api (o shell chama /me); aqui é só para não piscar páginas protegidas.
export function proxy(request: NextRequest) {
  const hasSession = Boolean(request.cookies.get("mgr_session")?.value);
  const { pathname } = request.nextUrl;
  const isLogin = pathname === "/login";
  if (!hasSession && !isLogin) {
    return NextResponse.redirect(new URL("/login", request.url));
  }
  if (hasSession && isLogin) {
    return NextResponse.redirect(new URL("/", request.url));
  }
  return NextResponse.next();
}

export const config = {
  // Tudo menos a API (proxy de rewrites), assets e ficheiros estáticos.
  matcher: ["/((?!api|_next/static|_next/image|favicon.ico|.*\\.(?:svg|png|jpg|ico)).*)"],
};
