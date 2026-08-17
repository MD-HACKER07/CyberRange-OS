"use client";

import { useState } from "react";
import useSWR from "swr";
import { fetcher } from "@/lib/api";
import { PageHeader } from "@/components/page-header";
import { Card, SectionTitle, Badge, Input, DataTable } from "@/components/ui";

interface Technique {
  technique_id: string;
  name: string;
  tactic: string;
  description: string;
}

export default function AttackPage() {
  const [search, setSearch] = useState("");
  const { data: status } = useSWR<{ technique_count: number; seeded: boolean }>("/attack/status", fetcher);
  const { data } = useSWR<{ items: Technique[] }>(`/attack/techniques?search=${encodeURIComponent(search)}`, fetcher);

  return (
    <div className="p-8">
      <PageHeader
        title="MITRE ATT&CK"
        subtitle={`Semantic auto-tagging backbone · ${status?.technique_count ?? 0} techniques seeded`}
      />
      <Card>
        <SectionTitle
          action={
            <Input
              placeholder="Search techniques…"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="w-64"
            />
          }
        >
          Techniques
        </SectionTitle>
        <DataTable
          rows={data?.items ?? []}
          columns={[
            { header: "ID", cell: (t) => <Badge accent="red">{t.technique_id}</Badge> },
            { header: "Name", cell: (t) => <span className="font-medium">{t.name}</span> },
            { header: "Tactic", cell: (t) => <Badge accent="slate">{t.tactic}</Badge> },
            {
              header: "Description",
              cell: (t) => <span className="text-vault-white/50 text-xs line-clamp-2">{t.description}</span>,
            },
          ]}
        />
      </Card>
    </div>
  );
}
