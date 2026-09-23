"use client";

import {
  Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis,
} from "recharts";

import { formatDate, formatNumber } from "@/lib/format";

// The manager's standard bar chart (Recharts — the same one the shadcn reference dashboard
// uses): lime-green bars with rounded tops and a MAXIMUM WIDTH (few points do not turn into giant
// blocks), axes without lines, a soft horizontal grid and a tooltip.
export function UsageChart({ data, format = formatNumber, height = 300 }: {
  data: { day: string; v: number }[];
  format?: (v: number) => string;
  height?: number;
}) {
  return (
    <ResponsiveContainer width="100%" height={height}>
      <BarChart data={data} margin={{ top: 8, right: 8, bottom: 0, left: 0 }}>
        <CartesianGrid vertical={false} strokeDasharray="3 3" stroke="var(--border)" />
        <XAxis dataKey="day" tickLine={false} axisLine={false} fontSize={12}
          stroke="var(--muted-foreground)"
          tickFormatter={(d: string) => formatDate(d).slice(0, 5)} />
        <YAxis tickLine={false} axisLine={false} fontSize={12} width={56}
          stroke="var(--muted-foreground)" allowDecimals={false}
          tickFormatter={(v: number) => format(v)} />
        <Tooltip
          cursor={{ fill: "var(--muted)", opacity: 0.5 }}
          contentStyle={{
            borderRadius: 8, border: "1px solid var(--border)",
            background: "var(--background)", fontSize: 12,
          }}
          formatter={(v) => [format(Number(v)), ""]}
          labelFormatter={(d) => formatDate(String(d))}
        />
        <Bar dataKey="v" fill="#a3e635" radius={[4, 4, 0, 0]} maxBarSize={40} />
      </BarChart>
    </ResponsiveContainer>
  );
}
