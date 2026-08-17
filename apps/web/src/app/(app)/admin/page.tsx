"use client";

import { useState } from "react";
import useSWR from "swr";
import { Server, Cpu, Users, ScrollText, ShieldCheck, Activity } from "lucide-react";
import { api, fetcher } from "@/lib/api";
import { PageHeader } from "@/components/page-header";
import { Card, SectionTitle, Button, Badge, Input, DataTable } from "@/components/ui";
import type { RangeTarget, LLMModel, EgressAssertion, ListResponse } from "@/lib/types";

type Tab = "health" | "targets" | "models" | "users" | "audit";

export default function AdminPanel() {
  const [tab, setTab] = useState<Tab>("health");
  const tabs: { id: Tab; label: string; icon: React.ReactNode }[] = [
    { id: "health", label: "Health", icon: <Activity size={16} /> },
    { id: "targets", label: "Range Targets", icon: <Server size={16} /> },
    { id: "models", label: "LLM Registry", icon: <Cpu size={16} /> },
    { id: "users", label: "Users & RBAC", icon: <Users size={16} /> },
    { id: "audit", label: "Audit Log", icon: <ScrollText size={16} /> },
  ];

  return (
    <div className="p-8">
      <PageHeader title="Admin Panel" subtitle="Provisioning, LLM registry, RBAC, and platform audit" />
      <div className="flex gap-1 mb-6 border-b border-vault-slate">
        {tabs.map((t) => (
          <button
            key={t.id}
            onClick={() => setTab(t.id)}
            className={`flex items-center gap-2 px-4 py-2 text-sm border-b-2 -mb-px ${
              tab === t.id ? "border-vault-gold text-vault-gold" : "border-transparent text-vault-white/60"
            }`}
          >
            {t.icon} {t.label}
          </button>
        ))}
      </div>
      {tab === "health" && <HealthTab />}
      {tab === "targets" && <TargetsTab />}
      {tab === "models" && <ModelsTab />}
      {tab === "users" && <UsersTab />}
      {tab === "audit" && <AuditTab />}
    </div>
  );
}

function HealthTab() {
  const { data } = useSWR<any>("/admin/health", fetcher, { refreshInterval: 8000 });
  const { data: egress, mutate } = useSWR<EgressAssertion>("/admin/llm-egress", fetcher);
  const verify = async () => {
    await api("/admin/llm-egress/verify", { method: "POST" });
    mutate();
  };
  return (
    <div className="grid md:grid-cols-2 gap-4">
      <Card accent={egress?.all_private ? "gold" : "red"}>
        <SectionTitle
          accent={egress?.all_private ? "gold" : "red"}
          action={
            <Button variant="outline" onClick={verify}>
              <ShieldCheck size={14} /> Verify Now
            </Button>
          }
        >
          LLM Egress Assertion
        </SectionTitle>
        {egress && (
          <div className="text-sm space-y-1">
            <div className="flex justify-between">
              <span className="text-vault-white/50">Endpoint</span>
              <span className="font-mono">{egress.endpoint}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-vault-white/50">Resolved</span>
              <span className="font-mono">{egress.resolved_ips?.join(", ")}</span>
            </div>
            <div className={`mt-2 p-2 rounded text-xs ${egress.all_private ? "bg-vault-gold/10 text-vault-gold" : "bg-vault-red/10 text-vault-red"}`}>
              {egress.message}
            </div>
          </div>
        )}
      </Card>
      <Card>
        <SectionTitle>System Health</SectionTitle>
        {data && (
          <div className="text-sm space-y-1">
            <Row k="Active sessions" v={data.active_sessions} />
            <Row k="Users" v={data.users} />
            <Row k="MITRE techniques" v={data.mitre_techniques} />
            <Row k="Range driver" v={`${data.range?.driver} ${data.range?.ok ? "✓" : "✗"}`} />
            <Row k="LLM" v={data.llm?.ok ? "reachable ✓" : `down: ${data.llm?.error ?? ""}`} />
            <Row k="Redis" v={data.redis ? "✓" : "✗"} />
          </div>
        )}
      </Card>
    </div>
  );
}

function TargetsTab() {
  const { data, mutate } = useSWR<ListResponse<RangeTarget>>("/range-targets", fetcher);
  const [form, setForm] = useState({ slug: "", name: "", image: "", ports: "", memory: 1024 });
  const create = async () => {
    await api("/admin/range-targets", {
      body: {
        slug: form.slug,
        name: form.name,
        image: form.image,
        exposed_ports: form.ports
          .split(",")
          .map((p) => parseInt(p.trim(), 10))
          .filter(Boolean),
        memory_mb: Number(form.memory),
      },
    });
    setForm({ slug: "", name: "", image: "", ports: "", memory: 1024 });
    mutate();
  };
  const remove = async (id: string) => {
    await api(`/admin/range-targets/${id}`, { method: "DELETE" });
    mutate();
  };
  return (
    <div className="grid md:grid-cols-3 gap-4">
      <Card className="md:col-span-2">
        <SectionTitle>Registered Targets</SectionTitle>
        <DataTable
          rows={data?.items ?? []}
          columns={[
            { header: "Slug", cell: (t) => <Badge accent="red">{t.slug}</Badge> },
            { header: "Image", cell: (t) => <span className="font-mono text-xs">{t.image}</span> },
            { header: "Ports", cell: (t) => <span className="text-xs">{t.exposed_ports.join(", ")}</span> },
            { header: "Active", cell: (t) => (t.is_active ? "✓" : "✗") },
            {
              header: "",
              cell: (t) => (
                <button onClick={() => remove(t.id)} className="text-vault-red text-xs hover:underline">
                  decommission
                </button>
              ),
            },
          ]}
        />
      </Card>
      <Card>
        <SectionTitle>Provision Target</SectionTitle>
        <div className="space-y-2">
          <Input placeholder="slug (e.g. dvwa)" value={form.slug} onChange={(e) => setForm({ ...form, slug: e.target.value })} />
          <Input placeholder="name" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
          <Input placeholder="docker image" value={form.image} onChange={(e) => setForm({ ...form, image: e.target.value })} />
          <Input placeholder="ports (comma-separated)" value={form.ports} onChange={(e) => setForm({ ...form, ports: e.target.value })} />
          <Button className="w-full" onClick={create}>
            Register Target
          </Button>
        </div>
      </Card>
    </div>
  );
}

function ModelsTab() {
  const { data, mutate } = useSWR<ListResponse<LLMModel>>("/admin/llm-models", fetcher);
  const [form, setForm] = useState({ name: "", endpoint: "", runtime: "ollama", modules: "" });
  const create = async () => {
    await api("/admin/llm-models", {
      body: {
        name: form.name,
        endpoint: form.endpoint,
        runtime: form.runtime,
        modules: form.modules.split(",").map((m) => m.trim()).filter(Boolean),
      },
    }).catch((e) => alert(e.message));
    mutate();
  };
  return (
    <div className="grid md:grid-cols-3 gap-4">
      <Card className="md:col-span-2">
        <SectionTitle>Model Registry</SectionTitle>
        <DataTable
          rows={data?.items ?? []}
          columns={[
            { header: "Name", cell: (m) => <span className="font-mono">{m.name}</span> },
            { header: "Endpoint", cell: (m) => <span className="font-mono text-xs">{m.endpoint}</span> },
            { header: "Modules", cell: (m) => <span className="text-xs">{m.modules.join(", ")}</span> },
            { header: "Default", cell: (m) => (m.is_default ? <Badge accent="gold">default</Badge> : "") },
          ]}
        />
      </Card>
      <Card>
        <SectionTitle>Register Local Model</SectionTitle>
        <div className="space-y-2">
          <Input placeholder="model name" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
          <Input placeholder="http://ollama:11434" value={form.endpoint} onChange={(e) => setForm({ ...form, endpoint: e.target.value })} />
          <Input placeholder="modules (comma sep)" value={form.modules} onChange={(e) => setForm({ ...form, modules: e.target.value })} />
          <Button className="w-full" onClick={create}>
            Register
          </Button>
          <p className="text-[10px] text-vault-white/40">Endpoint must resolve to a private address.</p>
        </div>
      </Card>
    </div>
  );
}

function UsersTab() {
  const { data, mutate } = useSWR<ListResponse<any>>("/users?limit=100", fetcher);
  const [form, setForm] = useState({ name: "", email: "", role: "student", roll_no: "", password: "" });
  const create = async () => {
    await api("/users", { body: form }).catch((e) => alert(e.message));
    setForm({ name: "", email: "", role: "student", roll_no: "", password: "" });
    mutate();
  };
  const setRole = async (id: string, role: string) => {
    await api(`/users/${id}/role`, { method: "PUT", body: { role } });
    mutate();
  };
  return (
    <div className="grid md:grid-cols-3 gap-4">
      <Card className="md:col-span-2">
        <SectionTitle>Users</SectionTitle>
        <DataTable
          rows={data?.items ?? []}
          columns={[
            { header: "Name", cell: (u) => u.name },
            { header: "Email", cell: (u) => <span className="text-xs">{u.email}</span> },
            {
              header: "Role",
              cell: (u) => (
                <select
                  value={u.role}
                  onChange={(e) => setRole(u.id, e.target.value)}
                  className="bg-black border border-vault-slate rounded px-2 py-1 text-xs"
                >
                  {["student", "faculty", "admin", "auditor"].map((r) => (
                    <option key={r} value={r}>
                      {r}
                    </option>
                  ))}
                </select>
              ),
            },
          ]}
        />
      </Card>
      <Card>
        <SectionTitle>Create User</SectionTitle>
        <div className="space-y-2">
          <Input placeholder="name" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
          <Input placeholder="email" value={form.email} onChange={(e) => setForm({ ...form, email: e.target.value })} />
          <Input placeholder="roll no (students)" value={form.roll_no} onChange={(e) => setForm({ ...form, roll_no: e.target.value })} />
          <select
            value={form.role}
            onChange={(e) => setForm({ ...form, role: e.target.value })}
            className="w-full bg-black border border-vault-slate rounded px-3 py-2 text-sm"
          >
            {["student", "faculty", "admin", "auditor"].map((r) => (
              <option key={r} value={r}>
                {r}
              </option>
            ))}
          </select>
          <Input
            type="password"
            placeholder="temp password"
            value={form.password}
            onChange={(e) => setForm({ ...form, password: e.target.value })}
          />
          <Button className="w-full" onClick={create}>
            Create
          </Button>
        </div>
      </Card>
    </div>
  );
}

function AuditTab() {
  const { data } = useSWR<ListResponse<any>>("/admin/audit-log?limit=200", fetcher, { refreshInterval: 10000 });
  return (
    <Card>
      <SectionTitle
        action={
          <a href="/api/admin/audit-log?format=csv&limit=1000" target="_blank" rel="noreferrer">
            <Button variant="outline">Export CSV</Button>
          </a>
        }
      >
        Append-only Audit Log
      </SectionTitle>
      <DataTable
        rows={data?.items ?? []}
        columns={[
          { header: "Time", cell: (r) => <span className="text-xs font-mono">{new Date(r.created_at).toLocaleString()}</span> },
          { header: "Actor", cell: (r) => <span className="text-xs">{r.actor_name || "system"}</span> },
          { header: "Action", cell: (r) => <Badge accent="slate">{r.action}</Badge> },
          { header: "Target", cell: (r) => <span className="text-xs font-mono">{r.target_type}</span> },
          {
            header: "Severity",
            cell: (r) => (
              <span className={r.severity === "critical" ? "text-vault-red" : "text-vault-white/60"}>{r.severity}</span>
            ),
          },
        ]}
      />
    </Card>
  );
}

function Row({ k, v }: { k: string; v: React.ReactNode }) {
  return (
    <div className="flex justify-between gap-2">
      <span className="text-vault-white/50">{k}</span>
      <span className="font-mono text-right">{v}</span>
    </div>
  );
}
