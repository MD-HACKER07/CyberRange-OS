"use client";

import { useState } from "react";
import useSWR from "swr";
import Link from "next/link";
import { api, fetcher } from "@/lib/api";
import { useAuth } from "@/lib/auth-context";
import { PageHeader } from "@/components/page-header";
import { Card, SectionTitle, Button, Badge, DataTable } from "@/components/ui";
import type { Report, ListResponse } from "@/lib/types";

interface COAttainment {
  co_code: string;
  description: string;
  target_percent: number;
  direct_score: number;
  indirect_score: number;
  final_score: number;
  attainment_level: number;
}

const levelColor = (l: number) =>
  l >= 3 ? "bg-vault-gold text-black-soft" : l === 2 ? "bg-vault-gold/60 text-black-soft" : l === 1 ? "bg-vault-red/60" : "bg-vault-red-deep";

export default function FacultyDashboard() {
  const { user } = useAuth();
  const [batchId, setBatchId] = useState(user?.batches?.[0]?.id ?? "");
  const { data: attain } = useSWR<{ outcomes: COAttainment[]; direct_weight: number; indirect_weight: number }>(
    batchId ? `/attainment/batch/${batchId}` : null,
    fetcher,
  );
  const { data: queue, mutate: mutateQueue } = useSWR<ListResponse<Report>>("/reports/grading-queue", fetcher);
  const { data: accuracy } = useSWR<{ ai_accuracy: number; student_accuracy: number; total_labeled: number }>(
    "/siem/accuracy",
    fetcher,
  );

  return (
    <div className="p-8">
      <PageHeader
        title="Faculty Dashboard"
        subtitle="Class attainment, grading queue, and SOC copilot accuracy"
        actions={
          user?.batches && user.batches.length > 0 ? (
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
          ) : undefined
        }
      />

      <div className="grid md:grid-cols-3 gap-4 mb-6">
        <Card>
          <div className="text-xs uppercase tracking-wide text-vault-white/50 mb-1">SOC Copilot Accuracy</div>
          <div className="font-display text-2xl text-vault-gold">
            {accuracy ? Math.round(accuracy.ai_accuracy * 100) : 0}%
          </div>
          <div className="text-xs text-vault-white/40">vs ground truth ({accuracy?.total_labeled ?? 0} labeled)</div>
        </Card>
        <Card>
          <div className="text-xs uppercase tracking-wide text-vault-white/50 mb-1">Student Triage Accuracy</div>
          <div className="font-display text-2xl text-vault-gold">
            {accuracy ? Math.round(accuracy.student_accuracy * 100) : 0}%
          </div>
        </Card>
        <Card>
          <div className="text-xs uppercase tracking-wide text-vault-white/50 mb-1">Grading Queue</div>
          <div className="font-display text-2xl text-vault-red">{queue?.total ?? 0}</div>
          <div className="text-xs text-vault-white/40">reports awaiting grade</div>
        </Card>
      </div>

      <Card className="mb-6">
        <SectionTitle
          action={
            <Link href={`/api/attainment/export/${batchId}`} target="_blank">
              <Button variant="outline">Export NBA CSV</Button>
            </Link>
          }
        >
          CO Attainment Heatmap
        </SectionTitle>
        <div className="space-y-1">
          {(attain?.outcomes ?? []).map((co) => (
            <div key={co.co_code} className="flex items-center gap-3">
              <span className="w-16 font-mono text-sm">{co.co_code}</span>
              <div className="flex-1 h-8 bg-vault-slate/40 rounded overflow-hidden relative">
                <div
                  className={`h-full ${levelColor(co.attainment_level)} flex items-center px-2 text-xs`}
                  style={{ width: `${Math.min(100, co.final_score)}%` }}
                >
                  {co.final_score.toFixed(1)}%
                </div>
              </div>
              <Badge accent={co.attainment_level >= 2 ? "gold" : "red"}>L{co.attainment_level}</Badge>
              <span className="text-xs text-vault-white/40 w-20">target {co.target_percent}%</span>
            </div>
          ))}
          {(attain?.outcomes ?? []).length === 0 && (
            <p className="text-sm text-vault-white/40">No attainment data yet. Grade some reports to populate.</p>
          )}
        </div>
        {attain && (
          <p className="text-xs text-vault-white/40 mt-2">
            Weighted {Math.round(attain.direct_weight * 100)}% direct / {Math.round(attain.indirect_weight * 100)}% indirect.
          </p>
        )}
      </Card>

      <Card>
        <SectionTitle>Grading Queue</SectionTitle>
        <DataTable
          rows={queue?.items ?? []}
          empty="Nothing to grade right now."
          columns={[
            {
              header: "Report",
              cell: (r) => (
                <Link href={`/reports/${r.id}`} className="text-vault-gold hover:underline">
                  {r.title}
                </Link>
              ),
            },
            { header: "Type", cell: (r) => <Badge accent={r.type === "pentest" ? "red" : "gold"}>{r.type}</Badge> },
            {
              header: "AI Suggestion",
              cell: (r) => (r.ai_suggested_score != null ? `~${Math.round(r.ai_suggested_score)}%` : "—"),
            },
          ]}
        />
      </Card>
    </div>
  );
}
