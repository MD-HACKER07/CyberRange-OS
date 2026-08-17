"use client";

import { useEffect, useState } from "react";
import { useParams } from "next/navigation";
import useSWR from "swr";
import { Save, Send, Download, Sparkles } from "lucide-react";
import { api, fetcher } from "@/lib/api";
import { useAuth } from "@/lib/auth-context";
import { PageHeader } from "@/components/page-header";
import { Card, SectionTitle, Button, Badge, Input, Textarea, Spinner } from "@/components/ui";
import { renderMarkdown } from "@/lib/markdown";
import type { Report } from "@/lib/types";

export default function ReportEditor() {
  const { id } = useParams<{ id: string }>();
  const { user } = useAuth();
  const { data: report, mutate } = useSWR<Report>(`/reports/${id}`, fetcher);
  const [title, setTitle] = useState("");
  const [content, setContent] = useState("");
  const [saving, setSaving] = useState(false);
  const [grading, setGrading] = useState(false);
  const [faculty, setFaculty] = useState({ score: 0, feedback: "" });

  useEffect(() => {
    if (report) {
      setTitle(report.title);
      setContent(report.content_md);
      setFaculty({ score: report.faculty_score ?? 0, feedback: report.faculty_feedback ?? "" });
    }
  }, [report?.id]); // eslint-disable-line react-hooks/exhaustive-deps

  if (!report) return <div className="p-8"><Spinner label="Loading report…" /></div>;

  const isOwner = user?.id === report.user_id;
  const isStaff = user?.role === "faculty" || user?.role === "admin";
  const editable = isOwner && (report.status === "draft" || report.status === "returned");

  const save = async () => {
    setSaving(true);
    try {
      await api(`/reports/${id}`, {
        method: "PUT",
        body: { title, content_md: content, technique_ids: report.technique_ids },
      });
      mutate();
    } finally {
      setSaving(false);
    }
  };

  const submit = async () => {
    await save();
    await api(`/reports/${id}/submit`, { method: "POST" });
    mutate();
  };

  const aiGrade = async () => {
    setGrading(true);
    try {
      await api(`/reports/${id}/ai-grade`, { body: {} });
      mutate();
    } finally {
      setGrading(false);
    }
  };

  const grade = async () => {
    await api(`/reports/${id}/grade`, { body: { score: faculty.score, feedback: faculty.feedback } });
    mutate();
  };

  return (
    <div className="p-8">
      <PageHeader
        title="Report Editor"
        subtitle={`${report.type} · ${report.status}`}
        actions={
          <div className="flex gap-2">
            <a href={`/api/reports/${id}/pdf`} target="_blank" rel="noreferrer">
              <Button variant="outline">
                <Download size={16} /> Export PDF
              </Button>
            </a>
            {editable && (
              <>
                <Button variant="ghost" onClick={save} disabled={saving}>
                  <Save size={16} /> {saving ? "Saving…" : "Save"}
                </Button>
                <Button onClick={submit}>
                  <Send size={16} /> Submit
                </Button>
              </>
            )}
          </div>
        }
      />

      <div className="grid grid-cols-2 gap-4">
        <Card>
          <SectionTitle>Markdown</SectionTitle>
          <Input value={title} onChange={(e) => setTitle(e.target.value)} className="mb-3" disabled={!editable} />
          <Textarea
            rows={26}
            value={content}
            onChange={(e) => setContent(e.target.value)}
            disabled={!editable}
            className="text-xs"
          />
          <div className="flex gap-1 flex-wrap mt-2">
            {report.technique_ids.map((t) => (
              <Badge key={t} accent="red">
                {t}
              </Badge>
            ))}
          </div>
        </Card>

        <Card>
          <SectionTitle>Live Preview</SectionTitle>
          <div
            className="prose prose-invert max-w-none text-sm overflow-auto"
            style={{ maxHeight: "42rem" }}
            dangerouslySetInnerHTML={{ __html: renderMarkdown(content) }}
          />
        </Card>
      </div>

      {isStaff && (
        <Card className="mt-4">
          <SectionTitle
            action={
              <Button variant="outline" onClick={aiGrade} disabled={grading}>
                <Sparkles size={14} /> {grading ? "Grading…" : "AI Suggest Grade"}
              </Button>
            }
          >
            Faculty Grading
          </SectionTitle>
          {report.ai_suggested_score != null && (
            <div className="mb-3 text-sm text-vault-white/60">
              AI-suggested: <span className="text-vault-gold">{Math.round(report.ai_suggested_score)}%</span> —{" "}
              {report.ai_score_rationale}
              <div className="text-[10px] text-vault-white/40">AI-suggested grade. Faculty score is authoritative.</div>
            </div>
          )}
          <div className="flex items-end gap-3">
            <div>
              <label className="text-xs text-vault-white/60 mb-1 block">Score (/{report.max_score})</label>
              <Input
                type="number"
                value={faculty.score}
                onChange={(e) => setFaculty({ ...faculty, score: Number(e.target.value) })}
                className="w-28"
              />
            </div>
            <div className="flex-1">
              <label className="text-xs text-vault-white/60 mb-1 block">Feedback</label>
              <Input value={faculty.feedback} onChange={(e) => setFaculty({ ...faculty, feedback: e.target.value })} />
            </div>
            <Button onClick={grade}>Save Grade</Button>
          </div>
        </Card>
      )}
    </div>
  );
}
