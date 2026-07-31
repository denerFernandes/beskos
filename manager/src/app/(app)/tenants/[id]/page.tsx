"use client";

import { use, useState } from "react";
import Link from "next/link";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api, ApiError } from "@/lib/api";
import { S, errorText } from "@/lib/strings";
import { Button } from "@/components/ui/button";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import { formatBytes, formatDate, formatDateTime, formatNumber } from "@/lib/format";
import { Badge } from "@/components/ui/badge";
import { Card, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from "@/components/ui/table";

// Detalhe de um tenant (M1, read-only): números + lista de utilizadores. Ações chegam na M2+.
export default function TenantDetailPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["tenant", id], queryFn: () => api.tenant(id) });
  const plansQ = useQuery({ queryKey: ["plans"], queryFn: () => api.plans() });
  const usageQ = useQuery({ queryKey: ["tenant-usage", id], queryFn: () => api.tenantUsage(id) });
  const t = q.data?.tenant;
  const users = q.data?.users ?? [];

  // Mudar o plano do tenant (M2): select + aplicar, com feedback inline.
  const [planPick, setPlanPick] = useState("");
  const [planMsg, setPlanMsg] = useState("");
  const [curPick, setCurPick] = useState("");
  const setCurrency = useMutation({
    mutationFn: (c: string) => api.setTenantCurrency(id, c),
    onSuccess: (_r, c) => { setCurPick(c || "default"); qc.invalidateQueries({ queryKey: ["tenant", id] }); },
  });
  const applyPlan = useMutation({
    mutationFn: () => api.setTenantPlan(id, planPick),
    onSuccess: () => {
      setPlanMsg(S.tenantPlan.applied);
      qc.invalidateQueries({ queryKey: ["tenant", id] });
      qc.invalidateQueries({ queryKey: ["tenants"] });
    },
    onError: (e) => setPlanMsg(errorText(e instanceof ApiError ? e.code : "manager.failed")),
  });
  const activePlans = (plansQ.data ?? []).filter((p) => p.active);
  const currentPlan = planPick || t?.plan || "";

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-1">
        <Link href="/tenants" className="text-sm text-muted-foreground hover:underline">
          {S.tenants.back}
        </Link>
        <div className="flex items-center gap-3">
          <h1 className="text-xl font-semibold tracking-tight">{t?.name ?? "…"}</h1>
          {t && <Badge variant="outline">{t.plan}</Badge>}
          {t && (
            <Badge variant={t.status === "active" ? "default" : "secondary"}>{t.status}</Badge>
          )}
        </div>
      </div>

      {q.isError && <p className="text-sm text-destructive">{S.dashboard.loadError}</p>}

      <div className="flex flex-wrap items-center gap-2">
        <span className="text-sm text-muted-foreground">{S.tenantPlan.label}:</span>
        <Select value={currentPlan} onValueChange={(v) => { setPlanPick(v ?? ""); setPlanMsg(""); }}>
          <SelectTrigger className="w-44"><SelectValue /></SelectTrigger>
          <SelectContent>
            {activePlans.map((p) => (
              <SelectItem key={p.key} value={p.key}>{p.name}</SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button size="sm" disabled={!planPick || planPick === t?.plan || applyPlan.isPending}
          onClick={() => applyPlan.mutate()}>
          {applyPlan.isPending ? S.tenantPlan.applying : S.tenantPlan.apply}
        </Button>
        {planMsg && <span className="text-sm text-muted-foreground">{planMsg}</span>}

        {/* Moeda de cobrança do tenant ('' = global). Assinatura existente mantém a moeda até à próxima troca. */}
        <span className="ml-4 text-sm text-muted-foreground">{S.tenantPlan.currency}:</span>
        <Select value={curPick || t?.currency || "default"} onValueChange={(v) => setCurrency.mutate(!v || v === "default" ? "" : v)}>
          <SelectTrigger className="w-28"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="default">{S.tenantPlan.currencyDefault}</SelectItem>
            {["BRL", "USD", "EUR", "PLN"].map((c) => <SelectItem key={c} value={c}>{c}</SelectItem>)}
          </SelectContent>
        </Select>
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <Card>
          <CardHeader>
            <CardDescription>{S.tenants.users}</CardDescription>
            <CardTitle className="text-2xl tabular-nums">{t ? formatNumber(t.users) : "…"}</CardTitle>
          </CardHeader>
        </Card>
        <Card>
          <CardHeader>
            <CardDescription>{S.tenants.domains}</CardDescription>
            <CardTitle className="text-2xl tabular-nums">{t ? formatNumber(t.domains) : "…"}</CardTitle>
          </CardHeader>
        </Card>
        <Card>
          <CardHeader>
            <CardDescription>{S.tenants.storage}</CardDescription>
            <CardTitle className="text-2xl tabular-nums">{t ? formatBytes(t.storage_bytes) : "…"}</CardTitle>
          </CardHeader>
        </Card>
        <Card>
          <CardHeader>
            <CardDescription>{S.tenants.apps}</CardDescription>
            <CardTitle className="text-sm font-normal">
              {t ? (t.disabled_apps.length ? t.disabled_apps.join(", ") : S.tenants.appsNone) : "…"}
            </CardTitle>
          </CardHeader>
        </Card>
      </div>

      <div className="flex flex-col gap-2">
        <h2 className="text-base font-semibold">{S.usage.title}</h2>
        <div className="rounded-md border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{S.usage.day}</TableHead>
                <TableHead className="text-right">{S.usage.users}</TableHead>
                <TableHead className="text-right">{S.usage.active}</TableHead>
                <TableHead className="text-right">{S.usage.storage}</TableHead>
                <TableHead className="text-right">{S.usage.aiTokens}</TableHead>
                <TableHead className="text-right">{S.usage.meetMin}</TableHead>
                <TableHead className="text-right">{S.usage.recordings}</TableHead>
                <TableHead className="text-right">{S.usage.wiki}</TableHead>
                <TableHead className="text-right">{S.usage.cards}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {(usageQ.data ?? []).map((u) => (
                <TableRow key={u.day}>
                  <TableCell className="tabular-nums">{formatDate(u.day)}</TableCell>
                  <TableCell className="text-right tabular-nums">{formatNumber(u.users)}</TableCell>
                  <TableCell className="text-right tabular-nums">{formatNumber(u.users_active)}</TableCell>
                  <TableCell className="text-right tabular-nums">{formatBytes(u.storage_bytes)}</TableCell>
                  <TableCell className="text-right tabular-nums">{formatNumber(u.ai_tokens)}</TableCell>
                  <TableCell className="text-right tabular-nums">{formatNumber(u.meet_minutes)}</TableCell>
                  <TableCell className="text-right tabular-nums">{formatNumber(u.recordings)}</TableCell>
                  <TableCell className="text-right tabular-nums">{formatNumber(u.wiki_pages)}</TableCell>
                  <TableCell className="text-right tabular-nums">{formatNumber(u.cards)}</TableCell>
                </TableRow>
              ))}
              {!usageQ.isPending && (usageQ.data ?? []).length === 0 && (
                <TableRow>
                  <TableCell colSpan={9} className="text-center text-muted-foreground">
                    {S.usage.empty}
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </div>
      </div>

      <div className="flex flex-col gap-2">
        <h2 className="text-base font-semibold">{S.tenants.usersTitle}</h2>
        <div className="rounded-md border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{S.tenants.email}</TableHead>
                <TableHead>{S.tenants.role}</TableHead>
                <TableHead>{S.tenants.mfa}</TableHead>
                <TableHead className="text-right">{S.tenants.storage}</TableHead>
                <TableHead className="text-right">{S.tenants.quota}</TableHead>
                <TableHead className="text-right">{S.tenants.lastSeen}</TableHead>
                <TableHead className="text-right">{S.tenants.created}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {users.map((u) => (
                <TableRow key={u.id}>
                  <TableCell>
                    <div className="font-medium">{u.email}</div>
                    {u.name && <div className="text-xs text-muted-foreground">{u.name}</div>}
                  </TableCell>
                  <TableCell><Badge variant="outline">{u.role}</Badge></TableCell>
                  <TableCell>
                    <Badge variant={u.mfa_enabled ? "default" : "secondary"}>
                      {u.mfa_enabled ? "✓" : "—"}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-right tabular-nums">{formatBytes(u.storage_bytes)}</TableCell>
                  <TableCell className="text-right tabular-nums">{formatBytes(u.quota_bytes)}</TableCell>
                  <TableCell className="text-right tabular-nums">{formatDateTime(u.mail_seen_at)}</TableCell>
                  <TableCell className="text-right tabular-nums">{formatDate(u.created_at)}</TableCell>
                </TableRow>
              ))}
              {!q.isPending && users.length === 0 && (
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
    </div>
  );
}
