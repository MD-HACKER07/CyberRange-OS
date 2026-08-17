"use client";

import useSWR from "swr";
import { fetcher } from "@/lib/api";
import { PageHeader } from "@/components/page-header";
import { Card, SectionTitle, Badge } from "@/components/ui";

interface CertProgress {
  cert: string;
  total_objectives: number;
  covered: number;
  covered_codes: string[];
  gap_codes: string[];
}

export default function LearningPaths() {
  const { data } = useSWR<{ note: string; paths: CertProgress[] }>("/certifications/progress", fetcher);

  return (
    <div className="p-8">
      <PageHeader title="Learning Paths" subtitle="In-house competency mapping — not an official certification" />
      <div className="grid md:grid-cols-3 gap-4">
        {(data?.paths ?? []).map((p) => {
          const pct = p.total_objectives ? Math.round((p.covered / p.total_objectives) * 100) : 0;
          return (
            <Card key={p.cert}>
              <SectionTitle>{p.cert}</SectionTitle>
              <div className="flex items-center justify-between text-sm mb-2">
                <span className="text-vault-white/60">
                  {p.covered}/{p.total_objectives} objectives
                </span>
                <span className="text-vault-gold font-mono">{pct}%</span>
              </div>
              <div className="h-2 bg-vault-slate rounded overflow-hidden mb-3">
                <div className="h-full bg-vault-gold" style={{ width: `${pct}%` }} />
              </div>
              {p.gap_codes.length > 0 && (
                <div>
                  <div className="text-xs text-vault-white/40 mb-1">Gaps:</div>
                  <div className="flex flex-wrap gap-1">
                    {p.gap_codes.slice(0, 8).map((c) => (
                      <Badge key={c} accent="slate">
                        {c}
                      </Badge>
                    ))}
                  </div>
                </div>
              )}
            </Card>
          );
        })}
        {(data?.paths ?? []).length === 0 && <p className="text-sm text-vault-white/40">No certification data seeded.</p>}
      </div>
    </div>
  );
}
