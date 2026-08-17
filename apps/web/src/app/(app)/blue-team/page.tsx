"use client";

import { useEffect, useState } from "react";
import useSWR from "swr";
import { Bot, ShieldAlert } from "lucide-react";
import { api, fetcher, wsURL } from "@/lib/api";
import { PageHeader } from "@/components/page-header";
import { Card, SectionTitle, Button, SeverityBadge, Badge, Textarea, Spinner } from "@/components/ui";
import type { Alert, ListResponse } from "@/lib/types";

interface CopilotResult {
  summary: string;
  verdict: string;
  confidence: number;
  reasoning: string;
  mitre_technique_id: string;
  next_step: string;
  incident_paragraph: string;
}

export default function BlueTeamConsole() {
  const { data, mutate } = useSWR<ListResponse<Alert>>("/siem/alerts?unresolved=true", fetcher, {
    refreshInterval: 10000,
  });
  const { data: metrics } = useSWR<{ mttd_seconds: number; mttr_seconds: number }>("/siem/metrics", fetcher, {
    refreshInterval: 15000,
  });
  const [selected, setSelected] = useState<Alert | null>(null);
  const [copilot, setCopilot] = useState<CopilotResult | null>(null);
  const [busy, setBusy] = useState(false);
  const [label, setLabel] = useState("");
  const [note, setNote] = useState("");

  // Live alert stream.
  useEffect(() => {
    const ws = new WebSocket(wsURL("/ws/siem/alerts/live"));
    ws.onmessage = (msg) => {
      try {
        const ev = JSON.parse(msg.data);
        if (ev.type === "alert.new" || ev.type === "alert.resolved") mutate();
      } catch {
        /* ignore */
      }
    };
    return () => ws.close();
  }, [mutate]);

  const select = async (a: Alert) => {
    setSelected(a);
    setCopilot(null);
    setLabel(a.student_label ?? "");
    setNote("");
    await api(`/siem/alerts/${a.id}/detect`, { method: "POST" }).catch(() => {});
  };

  const summarize = async () => {
    if (!selected) return;
    setBusy(true);
    try {
      const res = await api<CopilotResult>(`/siem/alerts/${selected.id}/copilot/summarize`, { body: {} });
      setCopilot(res);
      if (!label) setLabel(res.verdict);
      if (!note && res.incident_paragraph) setNote(res.incident_paragraph);
    } catch (e) {
      setCopilot({
        summary: `Copilot unavailable: ${e instanceof Error ? e.message : ""}`,
        verdict: "",
        confidence: 0,
        reasoning: "",
        mitre_technique_id: "",
        next_step: "",
        incident_paragraph: "",
      });
    } finally {
      setBusy(false);
    }
  };

  const resolve = async () => {
    if (!selected || !note.trim()) return;
    await api(`/siem/alerts/${selected.id}/resolve`, {
      body: { student_label: label, resolution_note: note },
    });
    setSelected(null);
    setCopilot(null);
    mutate();
  };

  const fmt = (s?: number) => (s && s > 0 ? `${Math.round(s)}s` : "—");

  return (
    <div className="p-6 h-screen flex flex-col">
      <PageHeader
        title="Blue Team SOC"
        subtitle="Live alert triage with AI copilot and MTTD/MTTR tracking"
        actions={
          <div className="flex gap-4 text-sm">
            <Badge accent="gold">MTTD {fmt(metrics?.mttd_seconds)}</Badge>
            <Badge accent="red">MTTR {fmt(metrics?.mttr_seconds)}</Badge>
          </div>
        }
      />
      <div className="flex-1 grid grid-cols-12 gap-3 min-h-0">
        {/* Left: alert feed */}
        <Card className="col-span-4 overflow-auto">
          <SectionTitle>
            <span className="flex items-center gap-2">
              <ShieldAlert size={14} /> Alert Feed
            </span>
          </SectionTitle>
          <div className="space-y-2">
            {(data?.items ?? []).map((a) => (
              <button
                key={a.id}
                onClick={() => select(a)}
                className={`w-full text-left border rounded p-2 transition-colors ${
                  selected?.id === a.id ? "border-vault-gold bg-vault-gold/5" : "border-vault-slate hover:bg-white/5"
                }`}
              >
                <div className="flex items-center justify-between">
                  <SeverityBadge severity={a.severity} />
                  <span className="text-xs text-vault-white/40">{a.source}</span>
                </div>
                <div className="text-sm mt-1 line-clamp-2">{a.rule_description || a.rule_id}</div>
                <div className="text-xs text-vault-white/40 font-mono mt-1">
                  {a.src_ip} {a.dst_ip ? `→ ${a.dst_ip}` : ""}
                </div>
              </button>
            ))}
            {(data?.items ?? []).length === 0 && (
              <p className="text-sm text-vault-white/40">No unresolved alerts. Range activity will populate this feed.</p>
            )}
          </div>
        </Card>

        {/* Center: detail + copilot */}
        <div className="col-span-5 flex flex-col gap-3 min-h-0">
          {!selected ? (
            <Card className="flex-1 flex items-center justify-center text-vault-white/40 text-sm">
              Select an alert to triage
            </Card>
          ) : (
            <>
              <Card className="overflow-auto">
                <SectionTitle>Alert Detail</SectionTitle>
                <div className="space-y-1 text-sm">
                  <Row k="Rule" v={`${selected.rule_id} — ${selected.rule_description}`} />
                  <Row k="Severity" v={selected.severity} />
                  <Row k="Source IP" v={selected.src_ip || "—"} />
                  <Row k="Dest IP" v={selected.dst_ip || "—"} />
                  {selected.mitre_technique_id && <Row k="ATT&CK" v={selected.mitre_technique_id} />}
                </div>
                <pre className="mt-2 bg-black rounded p-2 text-xs overflow-auto max-h-40 terminal-output">
                  {JSON.stringify(selected.raw_log, null, 2)}
                </pre>
              </Card>

              <Card className="flex-1 overflow-auto">
                <SectionTitle
                  action={
                    <Button variant="outline" onClick={summarize} disabled={busy}>
                      <Bot size={14} /> {busy ? "Analyzing…" : "AI Summarize"}
                    </Button>
                  }
                >
                  SOC Copilot
                </SectionTitle>
                {busy && <Spinner label="Querying local model…" />}
                {copilot && (
                  <div className="space-y-2 text-sm">
                    <p>{copilot.summary}</p>
                    {copilot.verdict && (
                      <div className="flex items-center gap-2">
                        <Badge accent={copilot.verdict === "false_positive" ? "slate" : "red"}>
                          {copilot.verdict} ({Math.round(copilot.confidence * 100)}%)
                        </Badge>
                        {copilot.mitre_technique_id && <Badge accent="gold">{copilot.mitre_technique_id}</Badge>}
                      </div>
                    )}
                    {copilot.reasoning && <p className="text-vault-white/60 text-xs">{copilot.reasoning}</p>}
                    {copilot.next_step && (
                      <p className="text-xs">
                        <span className="text-vault-gold">Next step:</span> {copilot.next_step}
                      </p>
                    )}
                    <p className="text-[10px] text-vault-white/40">AI-suggested, verify before submitting.</p>
                  </div>
                )}
              </Card>
            </>
          )}
        </div>

        {/* Right: resolution */}
        <Card className="col-span-3 overflow-auto">
          <SectionTitle>Triage Decision</SectionTitle>
          {selected ? (
            <div className="space-y-3">
              <div>
                <label className="text-xs text-vault-white/60 mb-1 block">Your verdict</label>
                <select
                  value={label}
                  onChange={(e) => setLabel(e.target.value)}
                  className="w-full bg-black border border-vault-slate rounded px-2 py-2 text-sm"
                >
                  <option value="">Select…</option>
                  <option value="true_positive">True Positive</option>
                  <option value="false_positive">False Positive</option>
                  <option value="benign">Benign</option>
                </select>
              </div>
              <div>
                <label className="text-xs text-vault-white/60 mb-1 block">Response / incident note</label>
                <Textarea rows={8} value={note} onChange={(e) => setNote(e.target.value)} placeholder="Document your response…" />
              </div>
              <Button className="w-full" onClick={resolve} disabled={!note.trim()}>
                Mark Resolved
              </Button>
            </div>
          ) : (
            <p className="text-sm text-vault-white/40">Select an alert first.</p>
          )}
        </Card>
      </div>
    </div>
  );
}

function Row({ k, v }: { k: string; v: string }) {
  return (
    <div className="flex justify-between gap-2">
      <span className="text-vault-white/50">{k}</span>
      <span className="font-mono text-right">{v}</span>
    </div>
  );
}
