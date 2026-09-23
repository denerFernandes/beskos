import { NextResponse } from "next/server";
import type { NextRequest } from "next/server";

// Fast cookie gate: no session → /login; session on /login → home. The REAL session validation
// belongs to manager-api (the shell calls /me); this only keeps protected pages from flashing.
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
  // Everything except the API (rewrites proxy), assets and static files.
  matcher: ["/((?!api|_next/static|_next/image|favicon.ico|.*\\.(?:svg|png|jpg|ico)).*)"],
};
