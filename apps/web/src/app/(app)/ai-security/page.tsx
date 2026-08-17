"use client";

import { useState } from "react";
import useSWR from "swr";
import { Brain, Play } from "lucide-react";
import { api, fetcher } from "@/lib/api";
import { PageHeader } from "@/components/page-header";
import { Card, SectionTitle, Button, Badge, DataTable } from "@/components/ui";
import type { AIScan, ListResponse } from "@/lib/types";

export default function AISecurityPage() {
  const { data, mutate } = useSWR<ListResponse<AIScan>>("/ai-security/results", fetcher, { refreshInterval: 5000 });
  const [tool, setTool] = useState("garak");
  const [busy, setBusy] = useState(false);

  const runScan = async () => {
    setBusy(true);
    try {
      await api("/ai-security/scan", { body: { tool } });
      mutate();
    } finally {
      setBusy(false);
    }
  };

  const scans = data?.items ?? [];
  const byCategory: Record<string, { pass: number; fail: number }> = {};
  for (const s of scans) {
    const c = s.probe_category || s.tool;
    byCategory[c] = byCategory[c] || { pass: 0, fail: 0 };
    if (s.passed) byCategory[c].pass++;
    else byCategory[c].fail++;
  }

  return (
    <div className="p-8">
      <PageHeader
        title="AI Security"
        subtitle="PyRIT / Garak probes run against our OWN local model — never a third-party system"
        actions={
          <div className="flex gap-2">
            <select
              value={tool}
              onChange={(e) => setTool(e.target.value)}
              className="bg-black border border-vault-slate rounded-md px-3 py-2 text-sm"
            >
              <option value="garak">Garak</option>
              <option value="pyrit">PyRIT</option>
            </select>
            <Button onClick={runScan} disabled={busy}>
              <Play size={16} /> {busy ? "Starting…" : "Run Scan"}
            </Button>
          </div>
        }
      />

      <div className="grid md:grid-cols-4 gap-4 mb-6">
        {Object.entries(byCategory).map(([cat, v]) => (
          <Card key={cat}>
            <div className="text-xs uppercase tracking-wide text-vault-white/50 mb-2 flex items-center gap-1">
              <Brain size={12} /> {cat}
            </div>
            <div className="flex items-center gap-3">
              <span className="text-vault-gold font-display text-xl">{v.pass}</span>
              <span className="text-vault-white/40 text-xs">pass</span>
              <span className="text-vault-red font-display text-xl">{v.fail}</span>
              <span className="text-vault-white/40 text-xs">fail</span>
            </div>
          </Card>
        ))}
        {Object.keys(byCategory).length === 0 && (
          <p className="text-sm text-vault-white/40">No scans yet. Run one to probe the local model&apos;s guardrails.</p>
        )}
      </div>

      <Card>
        <SectionTitle>Scan History</SectionTitle>
        <DataTable
          rows={scans}
          columns={[
            { header: "Tool", cell: (s) => <Badge accent="gold">{s.tool}</Badge> },
            { header: "Model", cell: (s) => <span className="font-mono text-xs">{s.model}</span> },
            { header: "Category", cell: (s) => s.probe_category || "—" },
            {
              header: "Result",
              cell: (s) =>
                s.status !== "completed" ? (
                  <Badge accent="slate">{s.status}</Badge>
                ) : s.passed ? (
                  <Badge accent="gold">all passed</Badge>
                ) : (
                  <Badge accent="red">
                    {s.failed_probes}/{s.total_probes} failed
                  </Badge>
                ),
            },
            { header: "When", cell: (s) => <span className="text-xs text-vault-white/40">{new Date(s.run_at).toLocaleString()}</span> },
          ]}
        />
      </Card>
    </div>
  );
}
