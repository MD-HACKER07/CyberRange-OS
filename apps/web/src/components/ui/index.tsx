"use client";

// Vault design system — shared, reusable components (spec Section 6).
// Card, Badge, Button, StatRing, Timer, DataTable, Terminal, AlertRow.
import { ReactNode, useEffect, useState } from "react";
import { cn } from "@/lib/cn";
import type { Severity } from "@/lib/types";

type Accent = "gold" | "red";

export function Card({
  children,
  accent = "gold",
  live = false,
  className,
}: {
  children: ReactNode;
  accent?: Accent;
  live?: boolean;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "rounded-lg bg-black-soft border p-4",
        accent === "gold" ? "border-vault-gold/30" : "border-vault-red/40",
        live && (accent === "gold" ? "shadow-glow-gold" : "shadow-glow-red"),
        className,
      )}
    >
      {children}
    </div>
  );
}

export function SectionTitle({
  children,
  accent = "gold",
  action,
}: {
  children: ReactNode;
  accent?: Accent;
  action?: ReactNode;
}) {
  return (
    <div className="flex items-center justify-between mb-3">
      <h2
        className={cn(
          "font-display text-sm uppercase tracking-widest",
          accent === "gold" ? "text-vault-gold" : "text-vault-red",
        )}
      >
        {children}
      </h2>
      {action}
    </div>
  );
}

export function Button({
  children,
  variant = "primary",
  className,
  ...props
}: {
  children: ReactNode;
  variant?: "primary" | "danger" | "ghost" | "outline";
} & React.ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button
      className={cn(
        "inline-flex items-center justify-center gap-2 rounded-md px-4 py-2 text-sm font-medium transition-colors disabled:opacity-40 disabled:cursor-not-allowed",
        variant === "primary" && "bg-vault-gold text-black-soft hover:bg-vault-gold-bright",
        variant === "danger" && "bg-vault-red text-vault-white hover:bg-vault-red-deep",
        variant === "ghost" && "text-vault-white/80 hover:bg-white/5",
        variant === "outline" && "border border-vault-gold/40 text-vault-gold hover:bg-vault-gold/10",
        className,
      )}
      {...props}
    >
      {children}
    </button>
  );
}

const sevStyles: Record<Severity, string> = {
  critical: "bg-vault-red-deep text-vault-white",
  high: "bg-vault-red text-black-soft",
  medium: "bg-vault-gold text-black-soft",
  low: "bg-vault-slate text-vault-white",
  info: "border border-vault-slate text-vault-white/70",
};

export function SeverityBadge({ severity }: { severity: Severity }) {
  return (
    <span className={cn("px-2 py-0.5 rounded text-xs font-semibold uppercase tracking-wide", sevStyles[severity])}>
      {severity}
    </span>
  );
}

export function Badge({ children, accent = "gold" }: { children: ReactNode; accent?: Accent | "slate" }) {
  return (
    <span
      className={cn(
        "px-2 py-0.5 rounded text-xs font-medium",
        accent === "gold" && "bg-vault-gold/15 text-vault-gold",
        accent === "red" && "bg-vault-red/15 text-vault-red",
        accent === "slate" && "bg-vault-slate/40 text-vault-white/70",
      )}
    >
      {children}
    </span>
  );
}

export function StatRing({
  value,
  max = 100,
  label,
  accent = "gold",
}: {
  value: number;
  max?: number;
  label: string;
  accent?: Accent;
}) {
  const pct = Math.max(0, Math.min(100, (value / max) * 100));
  const color = accent === "gold" ? "#D4AF37" : "#C41E3A";
  const r = 34;
  const circ = 2 * Math.PI * r;
  return (
    <div className="flex flex-col items-center gap-1">
      <svg width="88" height="88" className="-rotate-90">
        <circle cx="44" cy="44" r={r} stroke="#2E3440" strokeWidth="7" fill="none" />
        <circle
          cx="44"
          cy="44"
          r={r}
          stroke={color}
          strokeWidth="7"
          fill="none"
          strokeDasharray={circ}
          strokeDashoffset={circ - (pct / 100) * circ}
          strokeLinecap="round"
        />
      </svg>
      <div className="text-center -mt-14 mb-6">
        <div className="font-display text-lg" style={{ color }}>
          {Math.round(pct)}%
        </div>
      </div>
      <div className="text-xs text-vault-white/60 text-center">{label}</div>
    </div>
  );
}

export function Timer({ startISO, accent = "gold", label }: { startISO: string; accent?: Accent; label?: string }) {
  const [elapsed, setElapsed] = useState(0);
  useEffect(() => {
    const start = new Date(startISO).getTime();
    const tick = () => setElapsed(Math.max(0, Math.floor((Date.now() - start) / 1000)));
    tick();
    const id = setInterval(tick, 1000);
    return () => clearInterval(id);
  }, [startISO]);
  const h = Math.floor(elapsed / 3600);
  const m = Math.floor((elapsed % 3600) / 60);
  const s = elapsed % 60;
  const pad = (n: number) => String(n).padStart(2, "0");
  return (
    <div className="flex items-center gap-2">
      <span className={cn("w-2 h-2 rounded-full live-dot", accent === "gold" ? "bg-vault-gold" : "bg-vault-red")} />
      <span className="font-mono text-sm">
        {label ? `${label}: ` : ""}
        {h > 0 ? `${pad(h)}:` : ""}
        {pad(m)}:{pad(s)}
      </span>
    </div>
  );
}

export function Terminal({ children }: { children: ReactNode }) {
  return (
    <div className="bg-black rounded-md border border-vault-slate p-3 overflow-auto terminal-output text-vault-white/90 h-full">
      {children}
    </div>
  );
}

export function LiveDot({ accent = "red" }: { accent?: Accent }) {
  return (
    <span className={cn("inline-block w-2 h-2 rounded-full live-dot", accent === "gold" ? "bg-vault-gold" : "bg-vault-red")} />
  );
}

export function Spinner({ label }: { label?: string }) {
  return (
    <div className="flex items-center gap-2 text-vault-white/60 text-sm">
      <span className="w-4 h-4 border-2 border-vault-gold/40 border-t-vault-gold rounded-full animate-spin" />
      {label}
    </div>
  );
}

export function Input(props: React.InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      {...props}
      className={cn(
        "w-full bg-black border border-vault-slate rounded-md px-3 py-2 text-sm text-vault-white placeholder:text-vault-white/30 focus:border-vault-gold focus:outline-none",
        props.className,
      )}
    />
  );
}

export function Textarea(props: React.TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return (
    <textarea
      {...props}
      className={cn(
        "w-full bg-black border border-vault-slate rounded-md px-3 py-2 text-sm text-vault-white placeholder:text-vault-white/30 focus:border-vault-gold focus:outline-none font-mono",
        props.className,
      )}
    />
  );
}

export function DataTable<T>({
  rows,
  columns,
  empty = "No data",
}: {
  rows: T[];
  columns: { header: string; cell: (row: T) => ReactNode; className?: string }[];
  empty?: string;
}) {
  if (rows.length === 0) {
    return <div className="text-sm text-vault-white/40 py-6 text-center">{empty}</div>;
  }
  return (
    <div className="overflow-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="text-left text-vault-white/50 border-b border-vault-slate">
            {columns.map((c, i) => (
              <th key={i} className="py-2 px-2 font-medium">
                {c.header}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row, ri) => (
            <tr key={ri} className="border-b border-vault-slate/40 hover:bg-white/5">
              {columns.map((c, ci) => (
                <td key={ci} className={cn("py-2 px-2", c.className)}>
                  {c.cell(row)}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
