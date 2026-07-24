"use client";

import { useQuery } from "@tanstack/react-query";

import { api } from "@/lib/api";
import { S } from "@/lib/strings";
import { formatNumber } from "@/lib/format";
import { buttonVariants } from "@/components/ui/button";
import {
  Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from "@/components/ui/table";

// Relatórios (M5): crescimento mensal + exports CSV. Os exports são links diretos (o cookie de
// sessão vai junto — mesma origem).
const EXPORTS = [
  { href: "/api/manager/export/tenants.csv", label: S.reports.exportTenants },
  { href: "/api/manager/export/usage.csv?days=30", label: S.reports.exportUsage },
  { href: "/api/manager/export/finance.csv", label: S.reports.exportFinance },
];

export default function ReportsPage() {
  const growth = useQuery({ queryKey: ["growth"], queryFn: () => api.growth() });

  return (
    <div className="flex flex-col gap-6">
      <h1 className="text-xl font-semibold tracking-tight">{S.nav.reports}</h1>

      <div className="flex flex-col gap-2">
        <h2 className="text-base font-semibold">{S.reports.exports}</h2>
        <div className="flex flex-wrap gap-2">
          {EXPORTS.map((e) => (
            <a key={e.href} href={e.href} download
              className={buttonVariants({ variant: "outline", size: "sm" })}>
              {e.label}
            </a>
          ))}
        </div>
      </div>

      <div className="flex flex-col gap-2">
        <h2 className="text-base font-semibold">{S.reports.growth}</h2>
        <div className="rounded-md border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{S.reports.month}</TableHead>
                <TableHead className="text-right">{S.reports.tenantsNew}</TableHead>
                <TableHead className="text-right">{S.reports.usersNew}</TableHead>
                <TableHead className="text-right">{S.reports.tenantsTotal}</TableHead>
                <TableHead className="text-right">{S.reports.usersTotal}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {(growth.data ?? []).map((g) => (
                <TableRow key={g.month}>
                  <TableCell className="tabular-nums">{g.month}</TableCell>
                  <TableCell className="text-right tabular-nums">{formatNumber(g.tenants_new)}</TableCell>
                  <TableCell className="text-right tabular-nums">{formatNumber(g.users_new)}</TableCell>
                  <TableCell className="text-right tabular-nums">{formatNumber(g.tenants_total)}</TableCell>
                  <TableCell className="text-right tabular-nums">{formatNumber(g.users_total)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
        <p className="text-xs text-muted-foreground">{S.reports.churnNote}</p>
      </div>
    </div>
  );
}
