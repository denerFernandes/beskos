"use client";

import { useState } from "react";
import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import {
  AlertTriangle, Building2, CalendarDays, Download, HardDrive, Users, Wallet,
} from "lucide-react";

import { api } from "@/lib/api";
import { S } from "@/lib/strings";
import { formatBytes, formatDate, formatMoney, formatNumber } from "@/lib/format";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { buttonVariants } from "@/components/ui/button";
import {
  Card, CardContent, CardDescription, CardHeader, CardTitle,
} from "@/components/ui/card";
import {
  DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { UsageChart } from "@/components/usage-chart";
import {
  Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from "@/components/ui/table";

const RANGES = [7, 30, 90, 365];

// Dashboard faithful to the shadcn example with a real PERIOD FILTER (drives the charts and the
// export), an Analytics tab (storage/AI/Meet/email + top consumers) and excessive-usage alerts.
export default function DashboardPage() {
  const [days, setDays] = useState(30);
  const [tab, setTab] = useState("overview");

  const q = useQuery({ queryKey: ["overview"], queryFn: () => api.overview() });
  const fin = useQuery({ queryKey: ["finance"], queryFn: () => api.finance() });
  const series = useQuery({ queryKey: ["usage-series", days], queryFn: () => api.usageSeries(days) });
  const tenants = useQuery({ queryKey: ["tenants"], queryFn: () => api.tenants() });
  const alertsQ = useQuery({
    queryKey: ["alerts"], queryFn: () => api.alerts(),
    refetchInterval: 60_000, // alerts refresh on their own
  });
  const deliv = useQuery({ queryKey: ["deliverability"], queryFn: () => api.deliverability() });

  const d = q.data;
  const fs = fin.data?.summary;
  const pts = series.data ?? [];
  const recent = (tenants.data ?? []).slice(0, 5);
  const alerts = alertsQ.data?.alerts ?? [];
  const topAI = alertsQ.data?.top_ai_users ?? [];
  const topCost = [...(fin.data?.tenants ?? [])].sort((a, b) => b.cost_cents - a.cost_cents).slice(0, 5);

  const now = new Date();
  const from = new Date(now.getTime() - days * 86400000);
  const range = `${formatDate(from.toISOString())} - ${formatDate(now.toISOString())}`;

  const cards = [
    {
      label: S.dashboard.revenue, icon: Wallet,
      value: fs ? formatMoney(fs.mrr_cents, fs.currency) : "…",
      hint: fs ? `${S.finance.margin}: ${formatMoney(fs.margin_cents, fs.currency)}` : "",
    },
    {
      label: S.dashboard.tenants, icon: Building2,
      value: d ? `+${formatNumber(d.tenants)}` : "…",
      hint: d ? `${formatNumber(d.tenants_active)} ${S.dashboard.tenantsActive}` : "",
    },
    {
      label: S.dashboard.users, icon: Users,
      value: d ? `+${formatNumber(d.users)}` : "…",
      hint: d ? `${formatNumber(d.users_active_7d)} ${S.dashboard.usersActive}` : "",
    },
    {
      label: S.dashboard.storage, icon: HardDrive,
      value: d ? formatBytes(d.storage_bytes) : "…",
      hint: d ? `${formatNumber(d.domains)} ${S.dashboard.domains}` : "",
    },
  ];

  return (
    <div className="flex-1 space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-3xl font-bold tracking-tight">{S.dashboard.title}</h1>
        <div className="flex items-center gap-2">
          {/* REAL period filter: presets that drive the charts and the Download. */}
          <DropdownMenu>
            <DropdownMenuTrigger
              className={buttonVariants({ variant: "outline", size: "sm" }) + " tabular-nums"}>
              <CalendarDays className="mr-1.5 h-4 w-4" /> {range}
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              {RANGES.map((r) => (
                <DropdownMenuItem key={r} onClick={() => setDays(r)}
                  className={r === days ? "font-medium" : ""}>
                  {S.dashboard.lastNDays.replace("{n}", String(r))}
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
          <a href={`/api/manager/export/usage.csv?days=${days}`} download
            className={buttonVariants({ size: "sm" })}>
            <Download className="mr-1.5 h-4 w-4" /> {S.dashboard.download}
          </a>
        </div>
      </div>

      <Tabs value={tab} onValueChange={(v) => setTab(v ?? "overview")}>
        <TabsList>
          <TabsTrigger value="overview">{S.dashboard.tabOverview}</TabsTrigger>
          <TabsTrigger value="analytics">
            {S.dashboard.tabAnalytics}
            {alerts.length > 0 && (
              <Badge variant="destructive" className="ml-1.5 h-4 min-w-4 px-1 text-[10px]">
                {alerts.length}
              </Badge>
            )}
          </TabsTrigger>
        </TabsList>
      </Tabs>

      {q.isError && <p className="text-sm text-destructive">{S.dashboard.loadError}</p>}

      {tab === "overview" && (
        <>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
            {cards.map((c) => (
              <Card key={c.label} className="gap-2">
                <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-0">
                  <CardDescription className="text-sm font-medium text-foreground">
                    {c.label}
                  </CardDescription>
                  <c.icon className="h-4 w-4 text-muted-foreground" />
                </CardHeader>
                <CardContent>
                  <div className="text-2xl font-bold tabular-nums">{c.value}</div>
                  <p className="text-xs text-muted-foreground">{c.hint}</p>
                </CardContent>
              </Card>
            ))}
          </div>

          {alerts.length > 0 && <AlertsCard alerts={alerts} />}

          <div className="grid grid-cols-1 gap-4 lg:grid-cols-7">
            <Card className="lg:col-span-4">
              <CardHeader>
                <CardTitle className="text-base">{S.dashboard.chartActive}</CardTitle>
              </CardHeader>
              <CardContent className="pl-2">
                {pts.length > 0 ? (
                  <UsageChart data={pts.map((p) => ({ day: p.day, v: p.users_active }))} />
                ) : (
                  <p className="pl-4 text-sm text-muted-foreground">{S.usage.empty}</p>
                )}
              </CardContent>
            </Card>

            <Card className="lg:col-span-3">
              <CardHeader>
                <CardTitle className="text-base">{S.dashboard.recentTenants}</CardTitle>
                <CardDescription>
                  {d ? S.dashboard.recentHint.replace("{n}", formatNumber(d.tenants)) : ""}
                </CardDescription>
              </CardHeader>
              <CardContent className="flex flex-col gap-5">
                {recent.map((t) => (
                  <Link key={t.id} href={`/tenants/${t.id}`}
                    className="flex items-center gap-3 rounded-md transition-colors hover:bg-muted/60">
                    <Avatar className="h-9 w-9">
                      <AvatarFallback className="text-xs font-medium">
                        {t.name.slice(0, 2).toUpperCase()}
                      </AvatarFallback>
                    </Avatar>
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-sm font-medium leading-none">{t.name}</div>
                      <div className="mt-1 truncate text-sm text-muted-foreground">
                        {t.plan} · {formatDate(t.created_at)}
                      </div>
                    </div>
                    <div className="font-medium tabular-nums">
                      +{formatNumber(t.users)} {S.dashboard.usersShort}
                    </div>
                  </Link>
                ))}
              </CardContent>
            </Card>
          </div>
        </>
      )}

      {tab === "analytics" && (
        <>
          {alerts.length > 0 ? <AlertsCard alerts={alerts} /> : (
            <p className="text-sm text-muted-foreground">{S.dashboard.noAlerts}</p>
          )}

          <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
            <ChartCard title={S.dashboard.chartStorage}
              points={pts.map((p) => ({ day: p.day, v: p.storage_bytes }))} format={formatBytes} />
            <ChartCard title={S.dashboard.chartTokens}
              points={pts.map((p) => ({ day: p.day, v: p.ai_tokens }))} />
            <ChartCard title={S.dashboard.chartMeet}
              points={pts.map((p) => ({ day: p.day, v: p.meet_minutes }))} />
            <ChartCard title={S.dashboard.chartMail}
              points={pts.map((p) => ({ day: p.day, v: p.mail_sent }))} />
          </div>

          <Card>
            <CardHeader>
              <CardTitle className="text-base">{S.dashboard.deliverability}</CardTitle>
              <CardDescription>{S.dashboard.deliverabilityHint}</CardDescription>
            </CardHeader>
            <CardContent>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{S.finance.tenant}</TableHead>
                    <TableHead className="text-right">{S.dashboard.sent}</TableHead>
                    <TableHead className="text-right">{S.dashboard.bounces}</TableHead>
                    <TableHead className="text-right">{S.dashboard.rate}</TableHead>
                    <TableHead className="text-right">{S.dashboard.complaints}</TableHead>
                    <TableHead className="text-right">{S.dashboard.rate}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {(deliv.data ?? []).map((t) => {
                    const bp = t.sent_30d > 0 ? (t.bounces_30d * 100) / t.sent_30d : 0;
                    const cp = t.sent_30d > 0 ? (t.complaints_30d * 100) / t.sent_30d : 0;
                    return (
                      <TableRow key={t.id}>
                        <TableCell className="font-medium">{t.name}</TableCell>
                        <TableCell className="text-right tabular-nums">{formatNumber(t.sent_30d)}</TableCell>
                        <TableCell className="text-right tabular-nums">{formatNumber(t.bounces_30d)}</TableCell>
                        <TableCell className={`text-right tabular-nums ${bp >= 5 ? "text-destructive font-medium" : ""}`}>
                          {bp.toFixed(1)}%
                        </TableCell>
                        <TableCell className="text-right tabular-nums">{formatNumber(t.complaints_30d)}</TableCell>
                        <TableCell className={`text-right tabular-nums ${cp >= 0.1 ? "text-destructive font-medium" : ""}`}>
                          {cp.toFixed(2)}%
                        </TableCell>
                      </TableRow>
                    );
                  })}
                </TableBody>
              </Table>
            </CardContent>
          </Card>

          <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
            <Card>
              <CardHeader>
                <CardTitle className="text-base">{S.dashboard.topAI}</CardTitle>
                <CardDescription>{S.dashboard.topAIHint}</CardDescription>
              </CardHeader>
              <CardContent>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{S.tenants.email}</TableHead>
                      <TableHead>{S.finance.tenant}</TableHead>
                      <TableHead className="text-right">{S.usage.aiTokens}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {topAI.map((u) => (
                      <TableRow key={u.email}>
                        <TableCell className="font-medium">{u.email}</TableCell>
                        <TableCell className="text-muted-foreground">{u.tenant}</TableCell>
                        <TableCell className="text-right tabular-nums">{formatNumber(u.tokens)}</TableCell>
                      </TableRow>
                    ))}
                    {topAI.length === 0 && (
                      <TableRow>
                        <TableCell colSpan={3} className="text-center text-muted-foreground">
                          {S.usage.empty}
                        </TableCell>
                      </TableRow>
                    )}
                  </TableBody>
                </Table>
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle className="text-base">{S.dashboard.topCost}</CardTitle>
                <CardDescription>{S.dashboard.topCostHint}</CardDescription>
              </CardHeader>
              <CardContent>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{S.finance.tenant}</TableHead>
                      <TableHead className="text-right">{S.finance.cost}</TableHead>
                      <TableHead className="text-right">{S.finance.marginCol}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {topCost.map((t) => (
                      <TableRow key={t.id}>
                        <TableCell className="font-medium">{t.name}</TableCell>
                        <TableCell className="text-right tabular-nums">
                          {formatMoney(t.cost_cents, fs?.currency ?? "EUR")}
                        </TableCell>
                        <TableCell className={`text-right tabular-nums ${t.margin_cents < 0 ? "text-destructive" : ""}`}>
                          {formatMoney(t.margin_cents, fs?.currency ?? "EUR")}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </CardContent>
            </Card>
          </div>
        </>
      )}
    </div>
  );
}

// AlertsCard: excessive usage — badges by severity, link to the tenant when there is an id.
function AlertsCard({ alerts }: { alerts: import("@/lib/api").Alert[] }) {
  return (
    <Card className="border-amber-300/60">
      <CardHeader className="flex flex-row items-center gap-2 space-y-0">
        <AlertTriangle className="h-4 w-4 text-amber-500" />
        <CardTitle className="text-base">{S.dashboard.alertsTitle}</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-2">
        {alerts.map((a, i) => (
          <div key={i} className="flex flex-wrap items-center gap-2 text-sm">
            <Badge variant={a.severity === "critical" ? "destructive" : "secondary"}>
              {a.severity === "critical" ? S.dashboard.alertCritical : S.dashboard.alertWarn}
            </Badge>
            {a.tenant_id ? (
              <Link href={`/tenants/${a.tenant_id}`} className="font-medium hover:underline">
                {a.tenant}
              </Link>
            ) : (
              <span className="font-medium">{a.tenant}</span>
            )}
            <span className="text-muted-foreground">{a.detail}</span>
          </div>
        ))}
      </CardContent>
    </Card>
  );
}

function ChartCard({ title, points, format }: {
  title: string; points: { day: string; v: number }[]; format?: (v: number) => string;
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">{title}</CardTitle>
      </CardHeader>
      <CardContent className="pl-2">
        {points.length > 0 ? (
          <UsageChart data={points} format={format} height={200} />
        ) : (
          <p className="pl-4 text-sm text-muted-foreground">{S.usage.empty}</p>
        )}
      </CardContent>
    </Card>
  );
}
