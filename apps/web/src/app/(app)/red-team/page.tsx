"use client";

import { useEffect, useRef, useState, useCallback } from "react";
import useSWR from "swr";
import { Play, Square, Bot, Send, ChevronRight } from "lucide-react";
import { api, fetcher, wsURL } from "@/lib/api";
import { useAuth } from "@/lib/auth-context";
import { PageHeader } from "@/components/page-header";
import { Card, SectionTitle, Button, Badge, Terminal, Timer, Spinner, Input, LiveDot } from "@/components/ui";
import type { RangeSession, Exercise, CommandLogEntry, Suggestion, ListResponse, Batch } from "@/lib/types";

export default function RedTeamConsole() {
  const { user } = useAuth();
  const { data: active, mutate: mutateActive } = useSWR<{ session: RangeSession | null }>(
    "/range-sessions/active",
    fetcher,
    { refreshInterval: (d) => (d?.session?.status === "provisioning" ? 2000 : 0) },
  );
  const session = active?.session && active.session.status !== "completed" ? active.session : null;

  return (
    <div className="p-6 h-screen flex flex-col">
      <PageHeader title="Red Team Console" subtitle="Live offensive range with AI pentest copilot" />
      {!session ? (
        <ExercisePicker batches={user?.batches ?? []} onStarted={() => mutateActive()} />
      ) : (
        <SessionConsole session={session} onEnded={() => mutateActive()} />
      )}
    </div>
  );
}

function ExercisePicker({ batches, onStarted }: { batches: Batch[]; onStarted: () => void }) {
  const [batchId, setBatchId] = useState(batches[0]?.id ?? "");
  const { data } = useSWR<ListResponse<Exercise>>(
    batchId ? `/exercises?batch_id=${batchId}&type=red` : null,
    fetcher,
  );
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");

  const start = async (ex: Exercise) => {
    setBusy(ex.id);
    setError("");
    try {
      await api("/range-sessions", { body: { exercise_id: ex.id } });
      onStarted();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to start session");
    } finally {
      setBusy("");
    }
  };

  return (
    <Card accent="red" className="flex-1 overflow-auto">
      <SectionTitle accent="red">Start a Range Exercise</SectionTitle>
      {batches.length > 1 && (
        <select
          value={batchId}
          onChange={(e) => setBatchId(e.target.value)}
          className="bg-black border border-vault-slate rounded-md px-3 py-2 text-sm mb-4"
        >
          {batches.map((b) => (
            <option key={b.id} value={b.id}>
              {b.course_code} — {b.name}
            </option>
          ))}
        </select>
      )}
      {error && <div className="text-vault-red text-sm mb-3">{error}</div>}
      <div className="grid md:grid-cols-2 gap-3">
        {(data?.items ?? []).map((ex) => (
          <div key={ex.id} className="border border-vault-slate rounded-md p-4 flex flex-col">
            <div className="flex items-center justify-between">
              <span className="font-display">{ex.title}</span>
              <Badge accent="red">Difficulty {ex.difficulty}</Badge>
            </div>
            <p className="text-sm text-vault-white/50 mt-2 flex-1 line-clamp-3">{ex.brief_md}</p>
            <div className="flex items-center gap-2 mt-3 flex-wrap">
              {ex.target_image_refs.map((t) => (
                <Badge key={t} accent="slate">
                  {t}
                </Badge>
              ))}
            </div>
            <Button variant="danger" className="mt-3" onClick={() => start(ex)} disabled={busy === ex.id}>
              <Play size={16} /> {busy === ex.id ? "Provisioning…" : "Launch Range"}
            </Button>
          </div>
        ))}
        {(data?.items ?? []).length === 0 && (
          <p className="text-sm text-vault-white/40">No published red-team exercises in this batch.</p>
        )}
      </div>
    </Card>
  );
}

interface TermLine {
  kind: "cmd" | "out" | "sys";
  text: string;
  technique?: string | null;
  ai?: boolean;
}

function SessionConsole({ session, onEnded }: { session: RangeSession; onEnded: () => void }) {
  const provisioning = session.status === "provisioning";
  const [lines, setLines] = useState<TermLine[]>([]);
  const [suggestion, setSuggestion] = useState<Suggestion | null>(null);
  const [expectedOutcome, setExpectedOutcome] = useState("");
  const [copilotBusy, setCopilotBusy] = useState(false);
  const [manualCmd, setManualCmd] = useState("");
  const [editCmd, setEditCmd] = useState("");
  const [question, setQuestion] = useState("");
  const termRef = useRef<HTMLDivElement>(null);

  const { data: cmdLog, mutate: mutateLog } = useSWR<ListResponse<CommandLogEntry>>(
    `/range-sessions/${session.id}/commands`,
    fetcher,
  );
  const { data: techData, mutate: mutateTech } = useSWR<{ expected: string[]; demonstrated: string[] }>(
    `/range-sessions/${session.id}/techniques`,
    fetcher,
    { refreshInterval: 5000 },
  );

  // Seed terminal with historical command log.
  useEffect(() => {
    if (cmdLog?.items) {
      const seeded: TermLine[] = [];
      for (const e of cmdLog.items) {
        seeded.push({ kind: "cmd", text: e.command, ai: e.was_ai_suggested });
        seeded.push({ kind: "out", text: e.output, technique: e.mitre_technique_id });
      }
      setLines(seeded);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cmdLog?.items?.length]);

  // Live WS stream.
  useEffect(() => {
    if (provisioning) return;
    const ws = new WebSocket(wsURL(`/ws/range-sessions/${session.id}/terminal`));
    ws.onmessage = (msg) => {
      try {
        const ev = JSON.parse(msg.data);
        if (ev.type === "command.result") {
          const p = ev.payload as CommandLogEntry;
          setLines((prev) => [
            ...prev,
            { kind: "cmd", text: p.command, ai: p.was_ai_suggested },
            { kind: "out", text: p.output, technique: p.mitre_technique_id },
          ]);
          mutateTech();
        } else if (ev.type === "session.ended" || ev.type === "session.failed") {
          setLines((prev) => [...prev, { kind: "sys", text: `[session ${ev.type}]` }]);
          onEnded();
        }
      } catch {
        /* ignore */
      }
    };
    return () => ws.close();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [session.id, provisioning]);

  useEffect(() => {
    termRef.current?.scrollTo(0, termRef.current.scrollHeight);
  }, [lines]);

  const askCopilot = async () => {
    setCopilotBusy(true);
    try {
      const res = await api<{ suggestion: Suggestion; expected_outcome: string }>(
        `/range-sessions/${session.id}/copilot/suggest`,
        { body: { question } },
      );
      setSuggestion(res.suggestion);
      setEditCmd(res.suggestion.command);
      setExpectedOutcome(res.expected_outcome);
      setQuestion("");
    } catch (e) {
      setLines((prev) => [...prev, { kind: "sys", text: `[copilot error] ${e instanceof Error ? e.message : ""}` }]);
    } finally {
      setCopilotBusy(false);
    }
  };

  const approveRun = async () => {
    if (!suggestion) return;
    const modified = editCmd.trim() !== suggestion.command.trim();
    try {
      await api(`/range-sessions/${session.id}/copilot/execute`, {
        body: { suggestion_id: suggestion.id, command: modified ? editCmd : "" },
      });
      setSuggestion(null);
      setExpectedOutcome("");
      mutateLog();
    } catch (e) {
      setLines((prev) => [...prev, { kind: "sys", text: `[exec error] ${e instanceof Error ? e.message : ""}` }]);
    }
  };

  const runManual = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!manualCmd.trim()) return;
    const cmd = manualCmd;
    setManualCmd("");
    try {
      await api(`/range-sessions/${session.id}/exec`, { body: { command: cmd } });
      mutateLog();
    } catch (err) {
      setLines((prev) => [...prev, { kind: "sys", text: `[exec error] ${err instanceof Error ? err.message : ""}` }]);
    }
  };

  const endSession = useCallback(async () => {
    await api(`/range-sessions/${session.id}/end`, { method: "POST" });
    onEnded();
  }, [session.id, onEnded]);

  if (provisioning) {
    return (
      <Card accent="red" className="flex-1 flex items-center justify-center">
        <div className="text-center">
          <Spinner label="Provisioning isolated range (spinning up targets + Kali attacker)…" />
          <p className="text-xs text-vault-white/40 mt-3">
            Targets run on an internet-egress-denied network. This takes a moment.
          </p>
        </div>
      </Card>
    );
  }

  return (
    <div className="flex-1 grid grid-cols-12 gap-3 min-h-0">
      {/* Left: targets */}
      <div className="col-span-3 flex flex-col gap-3 min-h-0">
        <Card accent="red" className="flex-1 overflow-auto">
          <SectionTitle accent="red">Targets</SectionTitle>
          <div className="space-y-2">
            {(session.targets ?? []).map((t) => (
              <div key={t.id} className="border border-vault-red/30 rounded p-2 text-sm">
                <div className="font-mono text-vault-red">{t.hostname}</div>
                <div className="text-xs text-vault-white/50 font-mono">{t.ip_address || "…"}</div>
                <div className="text-xs text-vault-white/40 mt-1">{t.image}</div>
              </div>
            ))}
          </div>
          <div className="mt-4 flex items-center justify-between">
            {session.started_at && <Timer startISO={session.started_at} accent="red" />}
          </div>
          <Button variant="outline" className="w-full mt-3" onClick={endSession}>
            <Square size={14} /> End Session
          </Button>
        </Card>
      </div>

      {/* Center: terminal + copilot */}
      <div className="col-span-6 flex flex-col gap-3 min-h-0">
        <div className="flex items-center gap-2 text-xs text-vault-white/50">
          <LiveDot accent="red" /> kali-attacker · live
        </div>
        <div ref={termRef} className="flex-1 min-h-0">
          <Terminal>
            {lines.map((l, i) => (
              <div key={i}>
                {l.kind === "cmd" && (
                  <span className="text-vault-gold">
                    {l.ai ? "🤖 " : ""}$ {l.text}
                  </span>
                )}
                {l.kind === "out" && (
                  <span className="text-vault-white/80">
                    {l.text}
                    {l.technique && <span className="text-vault-red"> [{l.technique}]</span>}
                  </span>
                )}
                {l.kind === "sys" && <span className="text-vault-white/40 italic">{l.text}</span>}
              </div>
            ))}
          </Terminal>
        </div>
        <form onSubmit={runManual} className="flex gap-2">
          <Input
            value={manualCmd}
            onChange={(e) => setManualCmd(e.target.value)}
            placeholder="Type a command to run in Kali…"
            className="font-mono"
          />
          <Button type="submit" variant="outline">
            Run
          </Button>
        </form>
      </div>

      {/* Right: copilot + MITRE tracker */}
      <div className="col-span-3 flex flex-col gap-3 min-h-0">
        <Card accent="red" className="flex flex-col">
          <SectionTitle accent="red">
            <span className="flex items-center gap-2">
              <Bot size={14} /> Pentest Copilot
            </span>
          </SectionTitle>
          {suggestion ? (
            <div className="space-y-2">
              <div className="text-xs text-vault-white/60">{suggestion.rationale}</div>
              {suggestion.mitre_technique_id && <Badge accent="red">{suggestion.mitre_technique_id}</Badge>}
              <textarea
                value={editCmd}
                onChange={(e) => setEditCmd(e.target.value)}
                className="w-full bg-black border border-vault-slate rounded px-2 py-1 text-xs font-mono"
                rows={3}
              />
              {expectedOutcome && <div className="text-xs text-vault-white/40">Expected: {expectedOutcome}</div>}
              <div className="flex gap-2">
                <Button variant="danger" className="flex-1" onClick={approveRun}>
                  <ChevronRight size={14} /> Approve &amp; Run
                </Button>
                <Button variant="ghost" onClick={() => setSuggestion(null)}>
                  Dismiss
                </Button>
              </div>
              <p className="text-[10px] text-vault-white/40">AI-suggested. Review before you run.</p>
            </div>
          ) : (
            <div className="space-y-2">
              <Input
                value={question}
                onChange={(e) => setQuestion(e.target.value)}
                placeholder="Optional: ask the copilot…"
              />
              <Button variant="danger" className="w-full" onClick={askCopilot} disabled={copilotBusy}>
                {copilotBusy ? "Thinking…" : "Suggest Next Action"}
              </Button>
            </div>
          )}
        </Card>

        <Card accent="red" className="flex-1 overflow-auto">
          <SectionTitle accent="red">ATT&amp;CK Tracker</SectionTitle>
          <div className="space-y-1">
            {(techData?.expected ?? []).map((t) => {
              const done = techData?.demonstrated?.includes(t);
              return (
                <div key={t} className="flex items-center gap-2 text-sm">
                  <span className={done ? "text-vault-gold" : "text-vault-white/30"}>{done ? "●" : "○"}</span>
                  <span className={done ? "text-vault-white" : "text-vault-white/40"}>{t}</span>
                </div>
              );
            })}
            {(techData?.demonstrated ?? [])
              .filter((t) => !(techData?.expected ?? []).includes(t))
              .map((t) => (
                <div key={t} className="flex items-center gap-2 text-sm">
                  <span className="text-vault-red">+</span>
                  <span className="text-vault-white/70">{t}</span>
                </div>
              ))}
            {(techData?.expected ?? []).length === 0 && (techData?.demonstrated ?? []).length === 0 && (
              <p className="text-xs text-vault-white/40">Techniques light up as you work.</p>
            )}
          </div>
        </Card>
      </div>
    </div>
  );
}
