import { NextResponse, type NextRequest } from "next/server";
import { sessionCredential } from "./lib/session";
export async function proxy(request: NextRequest) {
  if (process.env.TRACEFORGE_REQUIRE_AUTH !== "1") return NextResponse.next();
  const path = request.nextUrl.pathname;
  if (path === "/login" || path === "/api/session") return NextResponse.next();
  if (await sessionCredential(request)) return NextResponse.next();
  if (path.startsWith("/api/"))
    return NextResponse.json(
      {
        error: { code: "session_required", message: "Authentication required" },
      },
      { status: 401 },
    );
  return NextResponse.redirect(new URL("/login", request.url));
}
export const config = {
  matcher: ["/((?!_next/static|_next/image|favicon.ico).*)"],
};
