"use client";

import { useState } from "react";
import useSWR from "swr";
import Link from "next/link";
import { FileText, Download, Plus } from "lucide-react";
import { api, fetcher } from "@/lib/api";
import { useAuth } from "@/lib/auth-context";
import { PageHeader } from "@/components/page-header";
import { Card, SectionTitle, Button, Badge, DataTable } from "@/components/ui";
import type { Report, ListResponse } from "@/lib/types";

export default function ReportsPage() {
  const { user } = useAuth();
  const { data, mutate } = useSWR<ListResponse<Report>>("/reports", fetcher);
  const [creating, setCreating] = useState(false);

  const createBlank = async () => {
    setCreating(true);
    try {
      const rep = await api<Report>("/reports", {
        body: { type: "pentest", title: "Untitled Report", content_md: "# Report\n\n" },
      });
      window.location.href = `/reports/${rep.id}`;
    } finally {
      setCreating(false);
    }
  };

  return (
    <div className="p-8">
      <PageHeader
        title="Reports"
        subtitle="Pentest and incident reports, graded against faculty rubrics"
        actions={
          <div className="flex gap-2">
            <Link href={`/reports/portfolio/${user?.id}`}>
              <Button variant="outline">
                <Download size={16} /> Portfolio PDF
              </Button>
            </Link>
            <Button onClick={createBlank} disabled={creating}>
              <Plus size={16} /> New Report
            </Button>
          </div>
        }
      />
      <Card>
        <SectionTitle>My Reports</SectionTitle>
        <DataTable
          rows={data?.items ?? []}
          empty="No reports yet. Start one from a completed range session."
          columns={[
            {
              header: "Title",
              cell: (r) => (
                <Link href={`/reports/${r.id}`} className="text-vault-gold hover:underline flex items-center gap-2">
                  <FileText size={14} /> {r.title}
                </Link>
              ),
            },
            { header: "Type", cell: (r) => <Badge accent={r.type === "pentest" ? "red" : "gold"}>{r.type}</Badge> },
            { header: "Status", cell: (r) => <Badge accent="slate">{r.status}</Badge> },
            {
              header: "Score",
              cell: (r) =>
                r.faculty_score != null ? (
                  <span className="text-vault-gold font-mono">
                    {r.faculty_score}/{r.max_score}
                  </span>
                ) : r.ai_suggested_score != null ? (
                  <span className="text-vault-white/40 font-mono">~{Math.round(r.ai_suggested_score)}</span>
                ) : (
                  "—"
                ),
            },
          ]}
        />
      </Card>
    </div>
  );
}
