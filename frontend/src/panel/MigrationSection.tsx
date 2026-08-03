// Bringing a mailbox in from the provider the customer is leaving.
//
// THE SCREEN IS TWO STEPS AND THAT IS THE POINT. Step one connects and shows what was found,
// copying nothing. Step two starts the job. Somebody handing over the password to their old mail
// account wants to see that we reached it and what we are going to do, before we do it -- and
// somebody who mistyped the server name finds out in three seconds instead of an hour.
//
// The folders that will NOT be copied are listed as prominently as the ones that will. Being told
// afterwards that Trash and Gmail's All Mail were skipped is being told after an afternoon of
// believing we lost the mail.

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, type Migration, type MigrationCheck, type MigrationFolder, type MigrationSource } from "../api";
import { toast } from "./ui";
import { IcDownload, IcCheck, IcX, IcRefresh } from "./icons";

export function MigrationSection() {
  const { t } = useTranslation();
  const current = useQuery({
    queryKey: ["migration"],
    queryFn: api.migration,
    // While a job runs the numbers are the whole screen. Polling stops the moment it ends, so a
    // finished migration is not a page that talks to the server every three seconds forever.
    refetchInterval: (q) => {
      const s = q.state.data?.migration?.status;
      return s === "running" || s === "pending" ? 3000 : false;
    },
  });

  if (current.isLoading) return <div className="loading">…</div>;
  const job = current.data?.migration ?? null;
  const live = job && (job.status === "running" || job.status === "pending");

  return (
    <div className="pnl-section">
      <h2>{t("migration.title")}</h2>
      <p className="desc">{t("migration.blurb")}</p>
      {live ? <MigrationProgress job={job!} /> : <MigrationForm last={job} />}
    </div>
  );
}

// ---- Step one and two: the form ----

function MigrationForm({ last }: { last: Migration | null }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [src, setSrc] = useState<MigrationSource>({ host: "", username: "", password: "", starttls: false });
  const [found, setFound] = useState<MigrationCheck | null>(null);
  const [err, setErr] = useState("");

  const check = useMutation({
    mutationFn: () => api.migrationCheck(src),
    onSuccess: (d) => { setFound(d); setErr(""); },
    onError: (e) => { setFound(null); setErr((e as Error).message); },
  });
  const start = useMutation({
    mutationFn: () => api.migrationStart(src),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["migration"] }); toast(t("migration.started")); },
    onError: (e) => setErr((e as Error).message),
  });

  const ready = src.host.trim() && src.username.trim() && src.password;
  // Changing any field invalidates what we found: the list on screen would otherwise describe a
  // different account than the one about to be copied.
  const edit = (patch: Partial<MigrationSource>) => { setSrc({ ...src, ...patch }); setFound(null); setErr(""); };

  return (
    <>
      {last && <LastRun job={last} />}

      <div className="pnl-card mig-form">
        <div className="pnl-field">
          <label>{t("migration.host")}</label>
          <input className="pnl-input" value={src.host} autoComplete="off" spellCheck={false}
            placeholder={t("migration.hostPlaceholder")}
            onChange={(e) => edit({ host: e.target.value.trim() })} />
          <div className="pnl-note">{t("migration.hostHint")}</div>
        </div>

        <div className="pnl-field">
          <label>{t("migration.username")}</label>
          <input className="pnl-input" value={src.username} autoComplete="off" spellCheck={false}
            onChange={(e) => edit({ username: e.target.value.trim() })} />
        </div>

        <div className="pnl-field">
          <label>{t("migration.password")}</label>
          <input className="pnl-input" type="password" value={src.password} autoComplete="new-password"
            onChange={(e) => edit({ password: e.target.value })} />
          {/* Google and Microsoft both refuse an account password over IMAP when two-factor is on.
              Saying so here saves the support request that otherwise arrives as "wrong password". */}
          <div className="pnl-note">{t("migration.passwordHint")}</div>
        </div>

        <label className="mig-check">
          <input type="checkbox" checked={!!src.starttls} onChange={(e) => edit({ starttls: e.target.checked })} />
          <span>{t("migration.starttls")}</span>
        </label>

        {err && <p className="pnl-err">{err}</p>}

        <div className="mig-actions">
          <button className="pnl-btn" disabled={!ready || check.isPending} onClick={() => check.mutate()}>
            {check.isPending ? t("migration.checking") : t("migration.check")}
          </button>
          {found && (
            <button className="pnl-btn primary" disabled={start.isPending} onClick={() => start.mutate()}>
              <IcDownload size={15} /> {t("migration.start")}
            </button>
          )}
        </div>
      </div>

      {found && <FoundFolders found={found} />}
    </>
  );
}

// FoundFolders is the promise the migration is making, in full: what comes, what does not, and how
// many messages. Shown BEFORE anything is copied.
function FoundFolders({ found }: { found: MigrationCheck }) {
  const { t, i18n } = useTranslation();
  const n = new Intl.NumberFormat(i18n.language);
  const copied = found.folders.filter((f) => !f.skip_reason);
  const skipped = found.folders.filter((f) => f.skip_reason);

  return (
    <div className="pnl-card mig-found">
      <h3>{t("migration.foundTitle", { count: found.total_messages, total: n.format(found.total_messages) })}</h3>
      <ul className="mig-folders">
        {copied.map((f) => (
          <li key={f.remote_name}>
            <IcCheck size={14} />
            <span className="name">{f.remote_name}</span>
            <span className="arrow">→</span>
            <span className="dest">{destName(f, t)}</span>
            <span className="count">{n.format(f.messages ?? 0)}</span>
          </li>
        ))}
      </ul>

      {skipped.length > 0 && (
        <>
          <h3 className="mig-skip-h">{t("migration.skippedTitle")}</h3>
          <ul className="mig-folders skipped">
            {skipped.map((f) => (
              <li key={f.remote_name}>
                <IcX size={14} />
                <span className="name">{f.remote_name}</span>
                <span className="why">{t(`migration.skip.${f.skip_reason}`)}</span>
              </li>
            ))}
          </ul>
        </>
      )}
    </div>
  );
}

// ---- The job, while it runs ----

function MigrationProgress({ job }: { job: Migration }) {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const n = new Intl.NumberFormat(i18n.language);
  const done = job.copied_messages + job.skipped_messages + job.failed_messages;
  const pct = job.total_messages > 0 ? Math.min(100, Math.round((done / job.total_messages) * 100)) : 0;

  const cancel = useMutation({
    mutationFn: () => api.migrationCancel(job.id),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["migration"] }); toast(t("migration.cancelled")); },
    onError: (e) => toast((e as Error).message),
  });

  return (
    <div className="pnl-card mig-progress">
      <div className="mig-src">{job.source_username} · {job.source_host}</div>

      {/* 'pending' is a job nobody has picked up yet. Saying so beats a progress bar at zero, which
          reads as broken (law 2.9: waiting has to look different from stuck). */}
      <div className="mig-state">{t(job.status === "pending" ? "migration.queued" : "migration.running")}</div>

      <div className="mig-bar"><span style={{ width: `${pct}%` }} /></div>
      <div className="mig-counts">
        <b>{n.format(job.copied_messages)}</b> {t("migration.copied")}
        {job.total_messages > 0 && <> {t("migration.ofTotal", { total: n.format(job.total_messages) })}</>}
        {job.skipped_messages > 0 && <> · {n.format(job.skipped_messages)} {t("migration.alreadyHere")}</>}
        {job.failed_messages > 0 && <> · <span className="warn">{n.format(job.failed_messages)} {t("migration.failed")}</span></>}
      </div>

      {job.folders && job.folders.length > 0 && <FolderProgress folders={job.folders} />}

      {/* Cancelling keeps what already arrived. Deleting mail we just copied, on the way out, is not
          a cancellation -- and the button has to say so or nobody will press it. */}
      <p className="pnl-note">{t("migration.cancelKeeps")}</p>
      <button className="pnl-btn ghost" disabled={cancel.isPending} onClick={() => cancel.mutate()}>
        {t("migration.cancel")}
      </button>
    </div>
  );
}

function FolderProgress({ folders }: { folders: MigrationFolder[] }) {
  const { t, i18n } = useTranslation();
  const n = new Intl.NumberFormat(i18n.language);
  return (
    <ul className="mig-folders live">
      {folders.map((f) => (
        <li key={f.remote_name} className={f.status}>
          <span className="name">{f.remote_name}</span>
          {f.skip_reason
            ? <span className="why">{t(`migration.skip.${f.skip_reason}`)}</span>
            : <span className="count">{n.format(f.copied_messages ?? 0)} / {n.format(f.total_messages ?? 0)}</span>}
        </li>
      ))}
    </ul>
  );
}

// ---- What happened last time ----

function LastRun({ job }: { job: Migration }) {
  const { t, i18n } = useTranslation();
  const n = new Intl.NumberFormat(i18n.language);
  const when = job.finished_at
    ? new Intl.DateTimeFormat(i18n.language, { dateStyle: "medium", timeStyle: "short" }).format(new Date(job.finished_at))
    : "";
  const bad = job.status === "failed";

  return (
    <div className={"pnl-card mig-last" + (bad ? " bad" : "")}>
      <div className="mig-last-h">
        {bad ? <IcX size={15} /> : <IcRefresh size={15} />}
        <b>{t(`migration.state.${job.status}`)}</b>
        {when && <span className="when">{when}</span>}
      </div>
      <div className="mig-src">{job.source_username} · {job.source_host}</div>
      <div className="mig-counts">
        <b>{n.format(job.copied_messages)}</b> {t("migration.copied")}
        {job.skipped_messages > 0 && <> · {n.format(job.skipped_messages)} {t("migration.alreadyHere")}</>}
        {/* Messages the source would not hand over. Named on screen because a migration that quietly
            loses eleven out of forty thousand is worse than one that says so. */}
        {job.failed_messages > 0 && <> · <span className="warn">{n.format(job.failed_messages)} {t("migration.failed")}</span></>}
      </div>
      {/* The backend sends a key, never a sentence (law 4.4): whatever a foreign IMAP server says is
          in its own language, and it is for the log, not for this screen. */}
      {bad && job.error_code && <p className="pnl-err">{t(`server.${job.error_code}`)}</p>}
    </div>
  );
}

// destName is where a folder lands: a role the account already has, or a folder created by name.
function destName(f: MigrationFolder, t: (k: string) => string): string {
  if (f.local_role) return t(`migration.role.${f.local_role}`);
  return f.local_name || f.remote_name;
}
