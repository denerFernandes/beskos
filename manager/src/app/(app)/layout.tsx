"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { ChevronsUpDown } from "lucide-react";

import { api, ApiError } from "@/lib/api";
import { S } from "@/lib/strings";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import {
  DropdownMenu, DropdownMenuContent, DropdownMenuGroup, DropdownMenuItem, DropdownMenuLabel,
  DropdownMenuSeparator, DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";

// Shell faithful to the shadcn/ui reference dashboard: team switcher (gradient dot + name +
// chevron), inline navigation, Search and avatar on the right. Validates the session at /me.
const NAV = [
  { href: "/", label: S.nav.dashboard },
  { href: "/tenants", label: S.nav.tenants },
  { href: "/plans", label: S.nav.plans },
  { href: "/finance", label: S.nav.finance },
  { href: "/reports", label: S.nav.reports },
  { href: "/waitlist", label: S.nav.waitlist },
  { href: "/operators", label: S.nav.operators },
];

export default function AppLayout({ children }: { children: React.ReactNode }) {
  const router = useRouter();
  const pathname = usePathname();

  const me = useQuery({ queryKey: ["me"], queryFn: () => api.me(), retry: false });
  if (me.error instanceof ApiError && me.error.status === 401) {
    router.replace("/login");
  }
  const email = me.data?.user.email ?? "";
  const name = me.data?.user.name || email.split("@")[0] || "…";
  const initials = (me.data?.user.name || email).slice(0, 2).toUpperCase() || "··";

  async function logout() {
    try {
      await api.logout();
    } finally {
      router.replace("/login");
    }
  }

  return (
    <div className="min-h-screen bg-background">
      <header className="border-b">
        <div className="flex h-16 items-center gap-4 px-4 sm:px-6">
          {/* Team switcher (visual): green gradient dot + operator name + chevron. */}
          <DropdownMenu>
            <DropdownMenuTrigger className="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm font-medium outline-none transition-colors hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring">
              <span className="h-4 w-4 rounded-full bg-gradient-to-br from-lime-400 to-emerald-600" />
              <span className="max-w-36 truncate">{name}</span>
              <ChevronsUpDown className="h-3.5 w-3.5 text-muted-foreground" />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start" className="min-w-52">
              <DropdownMenuGroup>
                <DropdownMenuLabel className="truncate text-xs font-normal text-muted-foreground">
                  {email || "…"}
                </DropdownMenuLabel>
              </DropdownMenuGroup>
              <DropdownMenuSeparator />
              <DropdownMenuItem onClick={logout}>{S.nav.logout}</DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>

          <nav className="flex items-center gap-4 overflow-x-auto text-sm lg:gap-6">
            {NAV.map((item) => {
              const active = item.href === "/" ? pathname === "/" : pathname.startsWith(item.href);
              return (
                <Link key={item.href} href={item.href}
                  className={cn(
                    "whitespace-nowrap font-medium transition-colors hover:text-primary",
                    active ? "text-foreground" : "text-muted-foreground",
                  )}>
                  {item.label}
                </Link>
              );
            })}
          </nav>

          <div className="ml-auto flex items-center gap-4">
            <SearchBox />
            <DropdownMenu>
              <DropdownMenuTrigger className="rounded-full outline-none focus-visible:ring-2 focus-visible:ring-ring">
                <Avatar className="h-9 w-9">
                  <AvatarFallback className="bg-foreground text-xs font-medium text-background">
                    {initials}
                  </AvatarFallback>
                </Avatar>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="min-w-48">
                <DropdownMenuGroup>
                  <DropdownMenuLabel className="truncate text-xs font-normal text-muted-foreground">
                    {email || "…"}
                  </DropdownMenuLabel>
                </DropdownMenuGroup>
                <DropdownMenuSeparator />
                <DropdownMenuItem onClick={logout}>{S.nav.logout}</DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </div>
      </header>
      <main className="flex-1 space-y-4 p-4 pt-6 sm:p-8">{children}</main>
    </div>
  );
}

// SearchBox: tenant search — Enter navigates to the filtered list.
function SearchBox() {
  const router = useRouter();
  return (
    <Input
      type="search"
      placeholder={S.nav.search}
      className="hidden w-56 md:block lg:w-72"
      onKeyDown={(e) => {
        if (e.key === "Enter") {
          const v = (e.target as HTMLInputElement).value.trim();
          router.push(v ? `/tenants?q=${encodeURIComponent(v)}` : "/tenants");
        }
      }}
    />
  );
}
