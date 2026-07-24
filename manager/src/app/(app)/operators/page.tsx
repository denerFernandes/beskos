"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api, ApiError, Operator } from "@/lib/api";
import { S, errorText } from "@/lib/strings";
import { formatDateTime } from "@/lib/format";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import { Sheet, SheetContent, SheetFooter, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import {
  Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from "@/components/ui/table";

// Operadores do manager (M5): lista + criar (Sheet, nunca diálogo nativo) + ativar/desativar.
// O TOTP obrigatório completa no primeiro login do novo operador.
export default function OperatorsPage() {
  const qc = useQueryClient();
  const ops = useQuery({ queryKey: ["operators"], queryFn: () => api.operators() });
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.me() });
  const [creating, setCreating] = useState(false);
  const [msg, setMsg] = useState("");

  const toggle = useMutation({
    mutationFn: (o: Operator) => api.setOperatorActive(o.id, !o.active),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["operators"] }),
    onError: (e) => setMsg(errorText(e instanceof ApiError ? e.code : "manager.failed")),
  });

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold tracking-tight">{S.nav.operators}</h1>
        <Button size="sm" onClick={() => setCreating(true)}>{S.operators.new}</Button>
      </div>
      {msg && <p className="text-sm text-muted-foreground">{msg}</p>}

      <div className="rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{S.operators.email}</TableHead>
              <TableHead>{S.operators.role}</TableHead>
              <TableHead>{S.operators.totp}</TableHead>
              <TableHead>{S.tenants.status}</TableHead>
              <TableHead className="text-right">{S.operators.lastLogin}</TableHead>
              <TableHead />
            </TableRow>
          </TableHeader>
          <TableBody>
            {(ops.data ?? []).map((o) => (
              <TableRow key={o.id}>
                <TableCell>
                  <div className="font-medium">{o.email}</div>
                  {o.name && <div className="text-xs text-muted-foreground">{o.name}</div>}
                </TableCell>
                <TableCell><Badge variant="outline">{o.role}</Badge></TableCell>
                <TableCell>
                  <Badge variant={o.totp_confirmed ? "default" : "secondary"}>
                    {o.totp_confirmed ? "✓" : S.operators.totpPending}
                  </Badge>
                </TableCell>
                <TableCell>
                  <Badge variant={o.active ? "default" : "secondary"}>
                    {o.active ? S.operators.active : S.operators.inactive}
                  </Badge>
                </TableCell>
                <TableCell className="text-right tabular-nums">{formatDateTime(o.last_login_at)}</TableCell>
                <TableCell className="text-right">
                  {me.data?.user.id !== o.id && (
                    <Button variant="outline" size="sm" disabled={toggle.isPending}
                      onClick={() => { setMsg(""); toggle.mutate(o); }}>
                      {o.active ? S.operators.deactivate : S.operators.activate}
                    </Button>
                  )}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>

      {creating && (
        <OperatorSheet onClose={() => setCreating(false)} onSaved={() => {
          qc.invalidateQueries({ queryKey: ["operators"] });
          setCreating(false);
        }} />
      )}
    </div>
  );
}

function OperatorSheet({ onClose, onSaved }: { onClose: () => void; onSaved: () => void }) {
  const [form, setForm] = useState({ email: "", name: "", password: "", role: "viewer" });
  const [error, setError] = useState("");

  const create = useMutation({
    mutationFn: () => api.createOperator(form),
    onSuccess: onSaved,
    onError: (e) => setError(errorText(e instanceof ApiError ? e.code : "manager.failed")),
  });

  return (
    <Sheet open onOpenChange={(open) => !open && onClose()}>
      <SheetContent className="flex w-full flex-col gap-0 sm:max-w-sm">
        <SheetHeader>
          <SheetTitle>{S.operators.new}</SheetTitle>
        </SheetHeader>
        <div className="flex flex-col gap-4 px-4 pb-4">
          <div className="grid gap-1.5">
            <Label htmlFor="o-email">{S.operators.email}</Label>
            <Input id="o-email" type="email" value={form.email}
              onChange={(e) => setForm({ ...form, email: e.target.value })} />
          </div>
          <div className="grid gap-1.5">
            <Label htmlFor="o-name">{S.operators.name}</Label>
            <Input id="o-name" value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })} />
          </div>
          <div className="grid gap-1.5">
            <Label htmlFor="o-pass">{S.operators.password}</Label>
            <Input id="o-pass" type="text" value={form.password}
              onChange={(e) => setForm({ ...form, password: e.target.value })} />
          </div>
          <div className="grid gap-1.5">
            <Label>{S.operators.role}</Label>
            <Select value={form.role} onValueChange={(v) => setForm({ ...form, role: v ?? "viewer" })}>
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="viewer">{S.operators.roleViewer}</SelectItem>
                <SelectItem value="admin">{S.operators.roleAdmin}</SelectItem>
              </SelectContent>
            </Select>
          </div>
          {error && <p className="text-sm text-destructive">{error}</p>}
        </div>
        <SheetFooter className="mt-auto flex-row justify-end gap-2 border-t px-4 py-3">
          <Button variant="outline" onClick={onClose}>{S.common.cancel}</Button>
          <Button disabled={create.isPending} onClick={() => { setError(""); create.mutate(); }}>
            {S.operators.create}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}
