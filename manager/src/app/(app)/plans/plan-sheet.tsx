"use client";

import { useState } from "react";
import { useMutation } from "@tanstack/react-query";

import { api, ApiError, Plan, PlanInput } from "@/lib/api";
import { S, errorText } from "@/lib/strings";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import { Sheet, SheetContent, SheetFooter, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";

const APPS = ["today", "mail", "chat", "kanban", "cal", "files", "contacts", "meet", "wiki"];

const EMPTY: PlanInput = {
  key: "", name: "", description: "", price_cents_per_user: 0, min_users: 0, max_users: 0,
  apps_enabled: [], max_storage_gb: 0, ai_tokens_per_window: 0, meet_max_participants: 0,
  meet_max_minutes: 0, recording: "none", recording_retention_days: 30, max_daily_send: 0,
  onlyoffice_edit: true, wiki_public_links: true, sort: 0,
};

// Sheet lateral de criação/edição de plano (regra da casa: sidecar, nunca diálogo nativo).
export function PlanSheet({ plan, onClose, onSaved }: {
  plan: Plan | null; onClose: () => void; onSaved: () => void;
}) {
  const [form, setForm] = useState<PlanInput>(plan ? { ...plan } : EMPTY);
  const [error, setError] = useState("");

  const save = useMutation({
    mutationFn: () => api.savePlan(form),
    onSuccess: (r) => {
      // Plano gravado; se o espelho na Stripe falhou, avisa em vez de fingir sucesso total.
      if (r.stripe_sync === "failed") setError("Plano salvo, mas a sincronização com a Stripe falhou — salve de novo para tentar outra vez.");
      else onSaved();
    },
    onError: (e) => setError(errorText(e instanceof ApiError ? e.code : "manager.failed")),
  });

  function num(field: keyof PlanInput) {
    return {
      type: "number" as const,
      value: String(form[field] as number),
      onChange: (e: React.ChangeEvent<HTMLInputElement>) =>
        setForm({ ...form, [field]: Number(e.target.value) || 0 }),
    };
  }

  function toggleApp(app: string, on: boolean) {
    const set = new Set(form.apps_enabled);
    if (on) set.add(app);
    else set.delete(app);
    setForm({ ...form, apps_enabled: APPS.filter((a) => set.has(a)) });
  }

  return (
    <Sheet open onOpenChange={(open) => !open && onClose()}>
      <SheetContent className="flex w-full flex-col gap-0 overflow-y-auto sm:max-w-md">
        <SheetHeader>
          <SheetTitle>{plan ? S.plans.edit : S.plans.new}</SheetTitle>
        </SheetHeader>

        <div className="flex flex-col gap-4 px-4 pb-4">
          <div className="grid grid-cols-2 gap-3">
            <div className="grid gap-1.5">
              <Label htmlFor="p-key">{S.plans.key}</Label>
              <Input id="p-key" value={form.key} disabled={!!plan}
                onChange={(e) => setForm({ ...form, key: e.target.value })} />
              {!plan && <p className="text-xs text-muted-foreground">{S.plans.keyHelp}</p>}
            </div>
            <div className="grid gap-1.5">
              <Label htmlFor="p-name">{S.plans.name}</Label>
              <Input id="p-name" value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })} />
            </div>
          </div>

          <div className="grid gap-1.5">
            <Label htmlFor="p-desc">{S.plans.description}</Label>
            <Textarea id="p-desc" rows={2} value={form.description}
              onChange={(e) => setForm({ ...form, description: e.target.value })} />
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div className="grid gap-1.5">
              <Label>{S.plans.price}</Label>
              <Input {...num("price_cents_per_user")} />
            </div>
            {/* Moeda por país: preço em centavos por moeda; 0 = usa o preço base. */}
            {(["BRL", "USD", "EUR", "PLN"] as const).map((cur) => (
              <div key={cur} className="grid gap-1.5">
                <Label>{S.plans.priceIn} {cur}</Label>
                <Input type="number" value={String(form.prices?.[cur] ?? 0)}
                  onChange={(e) => {
                    const v = Number(e.target.value) || 0;
                    const prices = { ...(form.prices ?? {}) };
                    if (v > 0) prices[cur] = v; else delete prices[cur];
                    setForm({ ...form, prices });
                  }} />
              </div>
            ))}
            <div className="grid gap-1.5">
              <Label>{S.plans.sort}</Label>
              <Input {...num("sort")} />
            </div>
            <div className="grid gap-1.5">
              <Label>{S.plans.minUsers}</Label>
              <Input {...num("min_users")} />
            </div>
            <div className="grid gap-1.5">
              <Label>{S.plans.maxUsers}</Label>
              <Input {...num("max_users")} />
            </div>
            <div className="grid gap-1.5">
              <Label>{S.plans.storage}</Label>
              <Input {...num("max_storage_gb")} />
            </div>
            <div className="grid gap-1.5">
              <Label>{S.plans.aiTokens}</Label>
              <Input {...num("ai_tokens_per_window")} />
            </div>
            <div className="grid gap-1.5">
              <Label>{S.plans.meetMax}</Label>
              <Input {...num("meet_max_participants")} />
            </div>
            <div className="grid gap-1.5">
              <Label>{S.plans.meetMinutes}</Label>
              <Input {...num("meet_max_minutes")} />
            </div>
            <div className="grid gap-1.5">
              <Label>{S.plans.dailySend}</Label>
              <Input {...num("max_daily_send")} />
            </div>
            <div className="grid gap-1.5">
              <Label>{S.plans.retention}</Label>
              <Input {...num("recording_retention_days")} />
            </div>
          </div>

          <div className="grid gap-1.5">
            <Label>{S.plans.recording}</Label>
            <Select value={form.recording}
              onValueChange={(v) => setForm({ ...form, recording: v as PlanInput["recording"] })}>
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="none">{S.plans.recordingNone}</SelectItem>
                <SelectItem value="offline">{S.plans.recordingOffline}</SelectItem>
                <SelectItem value="offline_priority">{S.plans.recordingPriority}</SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div className="flex items-center justify-between rounded-md border p-3">
            <Label htmlFor="p-oo">{S.plans.onlyoffice}</Label>
            <Switch id="p-oo" checked={form.onlyoffice_edit}
              onCheckedChange={(v) => setForm({ ...form, onlyoffice_edit: v })} />
          </div>
          <div className="flex items-center justify-between rounded-md border p-3">
            <Label htmlFor="p-wiki">{S.plans.wikiPublic}</Label>
            <Switch id="p-wiki" checked={form.wiki_public_links}
              onCheckedChange={(v) => setForm({ ...form, wiki_public_links: v })} />
          </div>

          <div className="grid gap-2">
            <Label>{S.plans.apps}</Label>
            <div className="grid grid-cols-3 gap-2 rounded-md border p-3">
              {APPS.map((app) => (
                <label key={app} className="flex items-center gap-2 text-sm">
                  <Checkbox checked={form.apps_enabled.includes(app)}
                    onCheckedChange={(v) => toggleApp(app, v === true)} />
                  {app}
                </label>
              ))}
            </div>
          </div>

          {error && <p className="text-sm text-destructive">{error}</p>}
        </div>

        <SheetFooter className="mt-auto flex-row justify-end gap-2 border-t px-4 py-3">
          <Button variant="outline" onClick={onClose}>{S.common.cancel}</Button>
          <Button disabled={save.isPending} onClick={() => { setError(""); save.mutate(); }}>
            {save.isPending ? S.plans.saving : S.plans.save}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}
