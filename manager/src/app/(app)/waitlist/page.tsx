"use client";

// The waiting list: who asked to be told, and the invitation.
//
// The column that matters is "what they are looking for". It is text somebody wrote of their own
// accord before being a customer, and it is the closest thing to a demand signal that exists at
// this stage -- putting it behind a click would hide the reason the list is kept at all.
//
// Inviting is irreversible: a message goes to somebody who is not yet a customer. So the button
// disappears the moment the state turns to "invited", rather than sitting there inviting a repeat.

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api, ApiError, WaitlistEntry } from "@/lib/api";
import { S, errorText } from "@/lib/strings";
import { formatDateTime } from "@/lib/format";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from "@/components/ui/table";

type Filter = "" | "pending" | "invited" | "joined";

const FILTERS: { key: Filter; label: string }[] = [
  { key: "", label: S.waitlist.all },
  { key: "pending", label: S.waitlist.pending },
  { key: "invited", label: S.waitlist.invitedStatus },
  { key: "joined", label: S.waitlist.joined },
];

export default function WaitlistPage() {
  const qc = useQueryClient();
  const [filter, setFilter] = useState<Filter>("");
  const [msg, setMsg] = useState("");
  const list = useQuery({ queryKey: ["waitlist", filter], queryFn: () => api.waitlist(filter) });

  const done = () => qc.invalidateQueries({ queryKey: ["waitlist"] });
  const fail = (e: unknown) => setMsg(errorText(e instanceof ApiError ? e.code : "manager.failed"));

  const invite = useMutation({
    mutationFn: (id: string) => api.inviteWaitlist(id),
    onSuccess: () => { done(); setMsg(S.waitlist.invited); },
    onError: fail,
  });
  const setStatus = useMutation({
    mutationFn: (v: { id: string; status: string }) => api.setWaitlistStatus(v.id, v.status),
    onSuccess: () => { done(); setMsg(""); },
    onError: fail,
  });
  const remove = useMutation({
    mutationFn: (id: string) => api.deleteWaitlist(id),
    onSuccess: () => { done(); setMsg(""); },
    onError: fail,
  });

  const counts = list.data?.counts;
  const entries = list.data?.entries ?? [];

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold tracking-tight">{S.waitlist.title}</h1>
        {counts && (
          <p className="text-sm text-muted-foreground">
            {counts.pending} {S.waitlist.counts} {counts.total}
          </p>
        )}
      </div>

      <div className="flex gap-2">
        {FILTERS.map((f) => (
          <Button key={f.key} size="sm" variant={filter === f.key ? "default" : "outline"}
            onClick={() => setFilter(f.key)}>
            {f.label}
          </Button>
        ))}
      </div>

      {msg && <p className="text-sm text-muted-foreground">{msg}</p>}

      <div className="rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{S.waitlist.email}</TableHead>
              <TableHead>{S.waitlist.who}</TableHead>
              <TableHead>{S.waitlist.note}</TableHead>
              <TableHead>{S.waitlist.source}</TableHead>
              <TableHead>{S.waitlist.status}</TableHead>
              <TableHead className="text-right">{S.waitlist.joinedAt}</TableHead>
              <TableHead />
            </TableRow>
          </TableHeader>
          <TableBody>
            {entries.map((e) => (
              <Row key={e.id} e={e}
                onInvite={() => invite.mutate(e.id)}
                inviting={invite.isPending && invite.variables === e.id}
                onJoined={() => setStatus.mutate({ id: e.id, status: "joined" })}
                onRemove={() => remove.mutate(e.id)} />
            ))}
            {entries.length === 0 && !list.isPending && (
              <TableRow>
                <TableCell colSpan={7} className="text-center text-sm text-muted-foreground">
                  {S.waitlist.empty}
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>
    </div>
  );
}

function Row({ e, onInvite, inviting, onJoined, onRemove }: {
  e: WaitlistEntry; onInvite: () => void; inviting: boolean; onJoined: () => void; onRemove: () => void;
}) {
  return (
    <TableRow>
      <TableCell>
        <div className="font-medium">{e.email}</div>
        {/* The locale decides which language the invitation goes out in, so it belongs beside the address. */}
        {e.locale && <div className="text-xs text-muted-foreground">{e.locale}</div>}
      </TableCell>
      <TableCell>
        <div>{e.name || "—"}</div>
        {e.company && <div className="text-xs text-muted-foreground">{e.company}</div>}
      </TableCell>
      {/* Not truncated: this is the text that says which segment is actually arriving. */}
      <TableCell className="max-w-sm whitespace-pre-wrap text-sm text-muted-foreground">
        {e.note || "—"}
      </TableCell>
      <TableCell className="text-sm text-muted-foreground">{e.source || "—"}</TableCell>
      <TableCell>
        <Badge variant={e.status === "joined" ? "default" : e.status === "invited" ? "secondary" : "outline"}>
          {e.status === "joined" ? S.waitlist.joined
            : e.status === "invited" ? S.waitlist.invitedStatus
              : S.waitlist.pending}
        </Badge>
        {e.invited_at && (
          <div className="mt-1 text-xs text-muted-foreground">
            {formatDateTime(e.invited_at)}{e.invited_by ? ` · ${e.invited_by}` : ""}
          </div>
        )}
      </TableCell>
      <TableCell className="text-right text-sm text-muted-foreground">
        {formatDateTime(e.created_at)}
      </TableCell>
      <TableCell className="text-right">
        <div className="flex justify-end gap-2">
          {/* Only while pending: an invitation already sent is not repeated by accident. */}
          {e.status === "pending" && (
            <Button size="sm" disabled={inviting} onClick={onInvite}>
              {inviting ? S.waitlist.inviting : S.waitlist.invite}
            </Button>
          )}
          {e.status === "invited" && (
            <Button size="sm" variant="outline" onClick={onJoined}>{S.waitlist.markJoined}</Button>
          )}
          <Button size="sm" variant="ghost" onClick={onRemove}>{S.waitlist.remove}</Button>
        </div>
      </TableCell>
    </TableRow>
  );
}
