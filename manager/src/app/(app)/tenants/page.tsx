"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { useQuery } from "@tanstack/react-query";

import { api } from "@/lib/api";
import { S } from "@/lib/strings";
import { formatBytes, formatDate, formatNumber } from "@/lib/format";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import {
  Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from "@/components/ui/table";

// Lista de tenants (M1, read-only): contagens por query direta; clicar abre o detalhe.
export default function TenantsPage() {
  const [q, setQ] = useState("");
  const tenants = useQuery({ queryKey: ["tenants"], queryFn: () => api.tenants() });

  // A Search do topo navega para /tenants?q=… — semeia o filtro a partir da URL.
  useEffect(() => {
    const initial = new URLSearchParams(window.location.search).get("q");
    if (initial) setQ(initial);
  }, []);

  const term = q.trim().toLowerCase();
  const rows = (tenants.data ?? []).filter((t) => !term || t.name.toLowerCase().includes(term));

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between gap-4">
        <h1 className="text-xl font-semibold tracking-tight">{S.nav.tenants}</h1>
        <Input className="max-w-xs" placeholder={S.tenants.search}
          value={q} onChange={(e) => setQ(e.target.value)} />
      </div>
      <div className="rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{S.tenants.name}</TableHead>
              <TableHead>{S.tenants.plan}</TableHead>
              <TableHead>{S.tenants.status}</TableHead>
              <TableHead className="text-right">{S.tenants.users}</TableHead>
              <TableHead className="text-right">{S.tenants.domains}</TableHead>
              <TableHead className="text-right">{S.tenants.storage}</TableHead>
              <TableHead className="text-right">{S.tenants.created}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((t) => (
              <TableRow key={t.id}>
                <TableCell>
                  <Link href={`/tenants/${t.id}`} className="font-medium hover:underline">
                    {t.name}
                  </Link>
                </TableCell>
                <TableCell><Badge variant="outline">{t.plan}</Badge></TableCell>
                <TableCell>
                  <Badge variant={t.status === "active" ? "default" : "secondary"}>{t.status}</Badge>
                </TableCell>
                <TableCell className="text-right tabular-nums">{formatNumber(t.users)}</TableCell>
                <TableCell className="text-right tabular-nums">{formatNumber(t.domains)}</TableCell>
                <TableCell className="text-right tabular-nums">{formatBytes(t.storage_bytes)}</TableCell>
                <TableCell className="text-right tabular-nums">{formatDate(t.created_at)}</TableCell>
              </TableRow>
            ))}
            {!tenants.isPending && rows.length === 0 && (
              <TableRow>
                <TableCell colSpan={7} className="text-center text-muted-foreground">
                  {S.tenants.empty}
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>
    </div>
  );
}
