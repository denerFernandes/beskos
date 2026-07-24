"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api, ApiError, CostModel, ManagerServer, ServerInput } from "@/lib/api";
import { S, errorText } from "@/lib/strings";
import { formatBytes, formatMoney, formatNumber } from "@/lib/format";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from "@/components/ui/table";
import { Sheet, SheetContent, SheetFooter, SheetHeader, SheetTitle } from "@/components/ui/sheet";

// Financeiro (M4): MRR/ARR/ARPU (receita contratada: preço do plano × utilizadores), custo
// variável estimado por driver e margem por tenant. Estimativas — a página avisa.
export default function FinancePage() {
  const qc = useQueryClient();
  const fin = useQuery({ queryKey: ["finance"], queryFn: () => api.finance() });
  const s = fin.data?.summary;
  const cur = s?.currency ?? "EUR";

  const cards = s
    ? [
        { label: S.finance.mrr, value: formatMoney(s.mrr_cents, cur), hint: `${formatNumber(s.paid_users)} ${S.finance.paidUsers}` },
        { label: S.finance.arr, value: formatMoney(s.arr_cents, cur), hint: "" },
        { label: S.finance.arpu, value: formatMoney(s.arpu_cents, cur), hint: "" },
        { label: S.finance.fixed, value: formatMoney(s.fixed_cents, cur),
          hint: S.finance.fixedBreakdown
            .replace("{s}", formatMoney(s.servers_cents, cur))
            .replace("{o}", formatMoney(s.other_fixed_cents, cur)) },
        { label: S.finance.variable, value: formatMoney(s.variable_cents, cur), hint: "" },
        { label: S.finance.margin, value: formatMoney(s.margin_cents, cur), hint: "" },
      ]
    : [];

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-1">
        <h1 className="text-xl font-semibold tracking-tight">{S.nav.finance}</h1>
        <p className="text-sm text-muted-foreground">{S.finance.estimate}</p>
      </div>
      {fin.isError && <p className="text-sm text-destructive">{S.dashboard.loadError}</p>}

      <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-6">
        {cards.map((c) => (
          <Card key={c.label}>
            <CardHeader>
              <CardDescription>{c.label}</CardDescription>
              <CardTitle className="text-xl tabular-nums">{c.value}</CardTitle>
              {c.hint && <CardDescription className="text-xs">{c.hint}</CardDescription>}
            </CardHeader>
          </Card>
        ))}
      </div>

      <div className="flex flex-col gap-2">
        <h2 className="text-base font-semibold">{S.finance.byTenant}</h2>
        <div className="rounded-md border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{S.finance.tenant}</TableHead>
                <TableHead>{S.finance.plan}</TableHead>
                <TableHead className="text-right">{S.finance.users}</TableHead>
                <TableHead className="text-right">{S.usage.storage}</TableHead>
                <TableHead className="text-right">{S.usage.aiTokens} 30d</TableHead>
                <TableHead className="text-right">{S.finance.revenue}</TableHead>
                <TableHead className="text-right">{S.finance.cost}</TableHead>
                <TableHead className="text-right">{S.finance.marginCol}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {(fin.data?.tenants ?? []).map((t) => (
                <TableRow key={t.id}>
                  <TableCell className="font-medium">{t.nome}</TableCell>
                  <TableCell><Badge variant="outline">{t.plano}</Badge></TableCell>
                  <TableCell className="text-right tabular-nums">{formatNumber(t.users)}</TableCell>
                  <TableCell className="text-right tabular-nums">{formatBytes(t.storage_bytes)}</TableCell>
                  <TableCell className="text-right tabular-nums">{formatNumber(t.ai_tokens_30d)}</TableCell>
                  <TableCell className="text-right tabular-nums">{formatMoney(t.mrr_cents, cur)}</TableCell>
                  <TableCell className="text-right tabular-nums">{formatMoney(t.cost_cents, cur)}</TableCell>
                  <TableCell className={`text-right tabular-nums ${t.margin_cents < 0 ? "text-destructive" : ""}`}>
                    {formatMoney(t.margin_cents, cur)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </div>

      <ServersCard onChanged={() => qc.invalidateQueries({ queryKey: ["finance"] })} currency={cur} />

      <CostModelCard onSaved={() => qc.invalidateQueries({ queryKey: ["finance"] })} />
    </div>
  );
}

// Editor do modelo de custos (admin-only no backend; o form mostra o erro se um viewer tentar).
function CostModelCard({ onSaved }: { onSaved: () => void }) {
  const [form, setForm] = useState<CostModel | null>(null);
  const [msg, setMsg] = useState("");
  const costs = useQuery({ queryKey: ["costs"], queryFn: () => api.costs() });
  const cm = form ?? costs.data ?? null;

  const save = useMutation({
    mutationFn: (c: CostModel) => api.saveCosts(c),
    onSuccess: () => { setMsg(S.finance.saved); onSaved(); },
    onError: (e) => setMsg(errorText(e instanceof ApiError ? e.code : "manager.failed")),
  });

  if (!cm) return null;
  const num = (field: keyof CostModel) => ({
    type: "number" as const,
    value: String(cm[field] as number),
    onChange: (e: React.ChangeEvent<HTMLInputElement>) =>
      setForm({ ...cm, [field]: Number(e.target.value) || 0 }),
  });

  return (
    <Card className="max-w-2xl">
      <CardHeader>
        <CardTitle className="text-base">{S.finance.costsTitle}</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
          <div className="grid gap-1.5">
            <Label>{S.finance.currency}</Label>
            <Input value={cm.currency}
              onChange={(e) => setForm({ ...cm, currency: e.target.value.toUpperCase() })} />
          </div>
          <div className="grid gap-1.5">
            <Label>{S.finance.otherFixed}</Label>
            <Input {...num("fixed_month_cents")} />
          </div>
          <div className="grid gap-1.5">
            <Label>{S.finance.storageGb}</Label>
            <Input {...num("storage_gb_month_cents")} />
          </div>
          <div className="grid gap-1.5">
            <Label>{S.finance.aiMtok}</Label>
            <Input {...num("ai_per_mtok_cents")} />
          </div>
          <div className="grid gap-1.5">
            <Label>{S.finance.email1k}</Label>
            <Input {...num("email_per_1k_cents")} />
          </div>
        </div>
        <div className="flex items-center gap-3">
          <Button size="sm" disabled={save.isPending} onClick={() => { setMsg(""); save.mutate(cm); }}>
            {S.finance.save}
          </Button>
          {msg && <span className="text-sm text-muted-foreground">{msg}</span>}
        </div>
      </CardContent>
    </Card>
  );
}


// Servidores da infra: custo fixo por VPS. Editar/criar em Sheet (regra da casa: sem diálogos nativos).
function ServersCard({ onChanged, currency }: { onChanged: () => void; currency: string }) {
  const qc = useQueryClient();
  const list = useQuery({ queryKey: ["servers"], queryFn: () => api.servers() });
  const [editing, setEditing] = useState<ManagerServer | "new" | null>(null);
  const [msg, setMsg] = useState("");

  const refresh = () => {
    qc.invalidateQueries({ queryKey: ["servers"] });
    onChanged();
  };
  const toggle = useMutation({
    mutationFn: (srv: ManagerServer) => api.setServerActive(srv.id, !srv.active),
    onSuccess: refresh,
    onError: (e) => setMsg(errorText(e instanceof ApiError ? e.code : "manager.failed")),
  });
  const del = useMutation({
    mutationFn: (id: string) => api.deleteServer(id),
    onSuccess: refresh,
    onError: (e) => setMsg(errorText(e instanceof ApiError ? e.code : "manager.failed")),
  });

  const total = (list.data ?? []).filter((x) => x.active).reduce((acc, x) => acc + x.monthly_cents, 0);

  return (
    <Card className="max-w-3xl">
      <CardHeader className="flex flex-row items-center justify-between">
        <div>
          <CardTitle className="text-base">{S.finance.serversTitle}</CardTitle>
          <CardDescription>{S.finance.serversHint}</CardDescription>
        </div>
        <Button size="sm" onClick={() => setEditing("new")}>{S.finance.serverNew}</Button>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{S.finance.serverName}</TableHead>
              <TableHead>{S.finance.serverProvider}</TableHead>
              <TableHead className="text-right">{S.finance.serverMonthlyCol}</TableHead>
              <TableHead>{S.finance.serverNotes}</TableHead>
              <TableHead />
            </TableRow>
          </TableHeader>
          <TableBody>
            {(list.data ?? []).map((srv) => (
              <TableRow key={srv.id} className={srv.active ? "" : "opacity-50"}>
                <TableCell className="font-medium">
                  <button className="hover:underline" onClick={() => setEditing(srv)}>{srv.name}</button>
                </TableCell>
                <TableCell className="text-muted-foreground">{srv.provider}</TableCell>
                <TableCell className="text-right tabular-nums">{formatMoney(srv.monthly_cents, currency)}</TableCell>
                <TableCell className="max-w-48 truncate text-muted-foreground">{srv.notes}</TableCell>
                <TableCell className="text-right whitespace-nowrap">
                  <Button variant="ghost" size="sm" disabled={toggle.isPending}
                    onClick={() => { setMsg(""); toggle.mutate(srv); }}>
                    {srv.active ? S.finance.serverPause : S.finance.serverResume}
                  </Button>
                  <Button variant="ghost" size="sm" className="text-destructive" disabled={del.isPending}
                    onClick={() => { setMsg(""); del.mutate(srv.id); }}>
                    {S.finance.serverDelete}
                  </Button>
                </TableCell>
              </TableRow>
            ))}
            <TableRow>
              <TableCell className="font-medium">Total</TableCell>
              <TableCell />
              <TableCell className="text-right font-medium tabular-nums">{formatMoney(total, currency)}</TableCell>
              <TableCell colSpan={2} />
            </TableRow>
          </TableBody>
        </Table>
        {msg && <p className="text-sm text-muted-foreground">{msg}</p>}
      </CardContent>

      {editing && (
        <ServerSheet server={editing === "new" ? null : editing}
          onClose={() => setEditing(null)}
          onSaved={() => { setEditing(null); refresh(); }} />
      )}
    </Card>
  );
}

function ServerSheet({ server, onClose, onSaved }: {
  server: ManagerServer | null; onClose: () => void; onSaved: () => void;
}) {
  const [form, setForm] = useState<ServerInput>(server
    ? { id: server.id, name: server.name, provider: server.provider, monthly_cents: server.monthly_cents, notes: server.notes }
    : { name: "", provider: "", monthly_cents: 0, notes: "" });
  const [error, setError] = useState("");

  const save = useMutation({
    mutationFn: () => api.saveServer(form),
    onSuccess: onSaved,
    onError: (e) => setError(errorText(e instanceof ApiError ? e.code : "manager.failed")),
  });

  return (
    <Sheet open onOpenChange={(open) => !open && onClose()}>
      <SheetContent className="flex w-full flex-col gap-0 sm:max-w-sm">
        <SheetHeader>
          <SheetTitle>{server ? S.finance.serverEdit : S.finance.serverNew}</SheetTitle>
        </SheetHeader>
        <div className="flex flex-col gap-4 px-4 pb-4">
          <div className="grid gap-1.5">
            <Label htmlFor="sv-name">{S.finance.serverName}</Label>
            <Input id="sv-name" value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })} />
          </div>
          <div className="grid gap-1.5">
            <Label htmlFor="sv-prov">{S.finance.serverProvider}</Label>
            <Input id="sv-prov" value={form.provider} placeholder="OVH VPS-3"
              onChange={(e) => setForm({ ...form, provider: e.target.value })} />
          </div>
          <div className="grid gap-1.5">
            <Label htmlFor="sv-cents">{S.finance.serverMonthly}</Label>
            <Input id="sv-cents" type="number" value={String(form.monthly_cents)}
              onChange={(e) => setForm({ ...form, monthly_cents: Number(e.target.value) || 0 })} />
          </div>
          <div className="grid gap-1.5">
            <Label htmlFor="sv-notes">{S.finance.serverNotes}</Label>
            <Input id="sv-notes" value={form.notes}
              onChange={(e) => setForm({ ...form, notes: e.target.value })} />
          </div>
          {error && <p className="text-sm text-destructive">{error}</p>}
        </div>
        <SheetFooter className="mt-auto flex-row justify-end gap-2 border-t px-4 py-3">
          <Button variant="outline" onClick={onClose}>{S.common.cancel}</Button>
          <Button disabled={save.isPending} onClick={() => { setError(""); save.mutate(); }}>
            {S.finance.save}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}
