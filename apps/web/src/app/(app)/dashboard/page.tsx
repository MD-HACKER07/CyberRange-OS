"use client";

import Link from "next/link";
import useSWR from "swr";
import { Swords, Radar, Activity } from "lucide-react";
import { useAuth } from "@/lib/auth-context";
import { fetcher } from "@/lib/api";
import { PageHeader } from "@/components/page-header";
import { Card, SectionTitle, Badge, Button, Timer, LiveDot } from "@/components/ui";
import type { RangeSession, ListResponse, LeaderboardRow, Batch } from "@/lib/types";

export default function Dashboard() {
  const { user } = useAuth();
  const { data: active } = useSWR<{ session: RangeSession | null }>("/range-sessions/active", fetcher);
  const batch = user?.batches?.[0];

  return (
    <div className="p-8">
      <PageHeader title={`Welcome, ${user?.name.split(" ")[0] ?? ""}`} subtitle="Your training overview" />

      {active?.session && active.session.status !== "completed" && (
        <Card accent="red" live className="mb-6">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-3">
              <LiveDot />
              <div>
                <div className="font-display text-vault-red uppercase text-sm tracking-widest">Active Range Session</div>
                <div className="text-sm text-vault-white/70 mt-1">Status: {active.session.status}</div>
              </div>
            </div>
            <div className="flex items-center gap-4">
              {active.session.started_at && <Timer startISO={active.session.started_at} accent="red" label="Elapsed" />}
              <Link href="/red-team">
                <Button variant="danger">Resume Console</Button>
              </Link>
            </div>
          </div>
        </Card>
      )}

      <div className="grid md:grid-cols-3 gap-4 mb-6">
        <Link href="/red-team">
          <Card accent="red" className="hover:border-vault-red transition-colors cursor-pointer h-full">
            <Swords className="text-vault-red mb-2" />
            <div className="font-display">Red Team Range</div>
            <p className="text-sm text-vault-white/50 mt-1">Recon, exploit, and report against live lab targets with an AI copilot.</p>
          </Card>
        </Link>
        <Link href="/blue-team">
          <Card accent="gold" className="hover:border-vault-gold transition-colors cursor-pointer h-full">
            <Radar className="text-vault-gold mb-2" />
            <div className="font-display">Blue Team SOC</div>
            <p className="text-sm text-vault-white/50 mt-1">Triage real Wazuh/Suricata alerts with MTTD/MTTR tracking.</p>
          </Card>
        </Link>
        <Link href="/reports">
          <Card accent="gold" className="hover:border-vault-gold transition-colors cursor-pointer h-full">
            <Activity className="text-vault-gold mb-2" />
            <div className="font-display">Reports & Portfolio</div>
            <p className="text-sm text-vault-white/50 mt-1">Write graded pentest/incident reports and export your portfolio PDF.</p>
          </Card>
        </Link>
      </div>

      <div className="grid md:grid-cols-2 gap-4">
        <Card>
          <SectionTitle>My Batches</SectionTitle>
          {user?.batches && user.batches.length > 0 ? (
            <div className="space-y-2">
              {user.batches.map((b: Batch) => (
                <div key={b.id} className="flex items-center justify-between text-sm">
                  <span>
                    {b.course_code} — {b.name}
                  </span>
                  <Badge accent="slate">{b.term || "current"}</Badge>
                </div>
              ))}
            </div>
          ) : (
            <p className="text-sm text-vault-white/40">You are not enrolled in any batch yet.</p>
          )}
        </Card>

        {batch && <LeaderboardSnippet batchId={batch.id} />}
      </div>
    </div>
  );
}

function LeaderboardSnippet({ batchId }: { batchId: string }) {
  const { data } = useSWR<ListResponse<LeaderboardRow>>(`/leaderboard/${batchId}?track=combined`, fetcher);
  return (
    <Card>
      <SectionTitle
        action={
          <Link href="/leaderboard" className="text-xs text-vault-gold hover:underline">
            View all
          </Link>
        }
      >
        Leaderboard
      </SectionTitle>
      <div className="space-y-2">
        {(data?.items ?? []).slice(0, 5).map((row) => (
          <div key={row.user_id} className="flex items-center justify-between text-sm">
            <span className="flex items-center gap-2">
              <span className="text-vault-gold font-mono w-6">#{row.rank}</span>
              {row.name}
            </span>
            <span className="font-mono text-vault-gold">{row.xp} XP</span>
          </div>
        ))}
        {(data?.items ?? []).length === 0 && <p className="text-sm text-vault-white/40">No ranked players yet.</p>}
      </div>
    </Card>
  );
}
