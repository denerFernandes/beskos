"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api, Plan } from "@/lib/api";
import { S } from "@/lib/strings";
import { formatNumber } from "@/lib/format";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from "@/components/ui/table";
import { PlanSheet } from "./plan-sheet";

// Catálogo de planos (M2). Editar abre um Sheet lateral (nunca diálogos nativos).
export default function PlansPage() {
  const qc = useQueryClient();
  const plansQ = useQuery({ queryKey: ["plans"], queryFn: () => api.plans() });
  const [editing, setEditing] = useState<Plan | "new" | null>(null);

  const toggleActive = useMutation({
    mutationFn: (p: Plan) => api.setPlanActive(p.key, !p.active),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["plans"] }),
  });

  const recordingLabel: Record<Plan["recording"], string> = {
    none: S.plans.recordingNone,
    offline: S.plans.recordingOffline,
    offline_priority: S.plans.recordingPriority,
  };

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold tracking-tight">{S.nav.plans}</h1>
        <Button size="sm" onClick={() => setEditing("new")}>{S.plans.new}</Button>
      </div>

      <div className="rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{S.plans.name}</TableHead>
              <TableHead className="text-right">{S.plans.priceCol}</TableHead>
              <TableHead className="text-right">{S.plans.usersCol}</TableHead>
              <TableHead className="text-right">{S.plans.storage}</TableHead>
              <TableHead>{S.plans.recordingCol}</TableHead>
              <TableHead>{S.tenants.status}</TableHead>
              <TableHead />
            </TableRow>
          </TableHeader>
          <TableBody>
            {(plansQ.data ?? []).map((p) => (
              <TableRow key={p.key} className="cursor-pointer" onClick={() => setEditing(p)}>
                <TableCell>
                  <div className="font-medium">{p.name}</div>
                  <div className="text-xs text-muted-foreground">{p.key}</div>
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {(p.price_cents_per_user / 100).toLocaleString("pt-BR", { minimumFractionDigits: 2 })}
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {p.min_users || 0}–{p.max_users ? formatNumber(p.max_users) : "∞"}
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {p.max_storage_gb ? `${formatNumber(p.max_storage_gb)} GB` : "∞"}
                </TableCell>
                <TableCell className="text-xs">{recordingLabel[p.recording]}</TableCell>
                <TableCell>
                  <Badge variant={p.active ? "default" : "secondary"}>
                    {p.active ? S.plans.active : S.plans.inactive}
                  </Badge>
                </TableCell>
                <TableCell className="text-right" onClick={(e) => e.stopPropagation()}>
                  <Button variant="outline" size="sm" disabled={toggleActive.isPending}
                    onClick={() => toggleActive.mutate(p)}>
                    {p.active ? S.plans.deactivate : S.plans.activate}
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>

      {editing && (
        <PlanSheet
          plan={editing === "new" ? null : editing}
          onClose={() => setEditing(null)}
          onSaved={() => {
            qc.invalidateQueries({ queryKey: ["plans"] });
            setEditing(null);
          }}
        />
      )}
    </div>
  );
}
