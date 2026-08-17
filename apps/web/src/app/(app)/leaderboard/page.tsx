"use client";

import { useState } from "react";
import useSWR from "swr";
import { fetcher } from "@/lib/api";
import { useAuth } from "@/lib/auth-context";
import { PageHeader } from "@/components/page-header";
import { Card, SectionTitle, Badge } from "@/components/ui";
import type { LeaderboardRow, ListResponse } from "@/lib/types";

const TRACKS = ["combined", "red", "blue"] as const;

export default function LeaderboardPage() {
  const { user } = useAuth();
  const [batchId, setBatchId] = useState(user?.batches?.[0]?.id ?? "");
  const [track, setTrack] = useState<(typeof TRACKS)[number]>("combined");
  const { data } = useSWR<ListResponse<LeaderboardRow>>(
    batchId ? `/leaderboard/${batchId}?track=${track}` : null,
    fetcher,
  );

  return (
    <div className="p-8">
      <PageHeader title="Leaderboard" subtitle="XP rewards independent skill; less copilot reliance scores higher" />
      <div className="flex gap-3 mb-4">
        {user?.batches && user.batches.length > 0 && (
          <select
            value={batchId}
            onChange={(e) => setBatchId(e.target.value)}
            className="bg-black border border-vault-slate rounded-md px-3 py-2 text-sm"
          >
            {user.batches.map((b) => (
              <option key={b.id} value={b.id}>
                {b.course_code} — {b.name}
              </option>
            ))}
          </select>
        )}
        <div className="flex gap-1">
          {TRACKS.map((t) => (
            <button
              key={t}
              onClick={() => setTrack(t)}
              className={`px-3 py-2 rounded-md text-sm capitalize ${
                track === t
                  ? t === "red"
                    ? "bg-vault-red/20 text-vault-red"
                    : "bg-vault-gold/20 text-vault-gold"
                  : "text-vault-white/60 hover:bg-white/5"
              }`}
            >
              {t}
            </button>
          ))}
        </div>
      </div>

      <Card accent={track === "red" ? "red" : "gold"}>
        <SectionTitle accent={track === "red" ? "red" : "gold"}>{track} ranking</SectionTitle>
        <div className="space-y-1">
          {(data?.items ?? []).map((row) => {
            const isMe = row.user_id === user?.id;
            return (
              <div
                key={row.user_id}
                className={`flex items-center justify-between px-3 py-2 rounded ${isMe ? "bg-vault-gold/10" : ""}`}
              >
                <div className="flex items-center gap-3">
                  <span
                    className={`font-mono w-8 ${row.rank <= 3 ? "text-vault-gold" : "text-vault-white/40"}`}
                  >
                    #{row.rank}
                  </span>
                  <span>{row.name}</span>
                  {row.roll_no && <Badge accent="slate">{row.roll_no}</Badge>}
                  {isMe && <Badge accent="gold">you</Badge>}
                </div>
                <span className="font-mono text-vault-gold">{row.xp} XP</span>
              </div>
            );
          })}
          {(data?.items ?? []).length === 0 && <p className="text-sm text-vault-white/40">No ranked players yet.</p>}
        </div>
      </Card>
    </div>
  );
}
