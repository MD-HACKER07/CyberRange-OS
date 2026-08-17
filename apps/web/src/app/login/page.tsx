"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { ShieldCheck, Terminal as TerminalIcon } from "lucide-react";
import { login, api } from "@/lib/api";
import { useAuth } from "@/lib/auth-context";
import { Button, Input } from "@/components/ui";

export default function LoginPage() {
  const router = useRouter();
  const { user, refresh } = useAuth();
  const [loginId, setLoginId] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [providers, setProviders] = useState<{ local: boolean; sso: boolean; sso_label: string }>({
    local: true,
    sso: false,
    sso_label: "",
  });

  useEffect(() => {
    if (user) router.replace("/dashboard");
  }, [user, router]);

  useEffect(() => {
    api<{ local: boolean; sso: boolean; sso_label: string }>("/auth/providers")
      .then(setProviders)
      .catch(() => {});
  }, []);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    setBusy(true);
    try {
      await login(loginId, password);
      await refresh();
      router.replace("/dashboard");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Login failed");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="min-h-screen grid md:grid-cols-2">
      {/* Left: brand / mission on black with animated network-graph feel */}
      <div className="relative hidden md:flex flex-col justify-between p-12 vault-grid border-r border-vault-slate overflow-hidden">
        <div className="absolute inset-0 vault-scanlines pointer-events-none" />
        <div className="relative z-10 flex items-center gap-2">
          <ShieldCheck className="text-vault-gold" />
          <span className="font-display text-vault-gold tracking-[0.3em] text-sm uppercase">CyberRange OS</span>
        </div>
        <div className="relative z-10 max-w-md">
          <h1 className="font-display text-4xl leading-tight mb-4">
            Train like the <span className="text-vault-red">adversary</span>.<br />
            Defend like a <span className="text-vault-gold">pro</span>.
          </h1>
          <p className="text-vault-white/60 text-sm leading-relaxed">
            A self-hosted red team / blue team range with a locally-hosted AI copilot. Every
            exercise is supervised, every action logged, and no student data ever leaves the
            institution&apos;s network.
          </p>
        </div>
        <div className="relative z-10 text-xs text-vault-white/40 font-mono">
          Local inference only · Egress-denied range · NBA/NAAC evidence built-in
        </div>
      </div>

      {/* Right: auth form */}
      <div className="flex items-center justify-center p-8">
        <div className="w-full max-w-sm">
          <div className="md:hidden flex items-center gap-2 mb-8">
            <ShieldCheck className="text-vault-gold" />
            <span className="font-display text-vault-gold tracking-widest text-sm uppercase">CyberRange OS</span>
          </div>
          <h2 className="font-display text-2xl mb-1">Sign in</h2>
          <p className="text-vault-white/50 text-sm mb-6">Use your institution credentials or lab account.</p>

          {providers.sso && (
            <a href="/api/auth/sso/start" className="block mb-4">
              <Button variant="outline" className="w-full" type="button">
                <TerminalIcon size={16} /> {providers.sso_label || "Institution SSO"}
              </Button>
            </a>
          )}

          {providers.local && (
            <form onSubmit={submit} className="space-y-4">
              <div>
                <label className="text-xs text-vault-white/60 mb-1 block">Email or Roll Number</label>
                <Input
                  value={loginId}
                  onChange={(e) => setLoginId(e.target.value)}
                  placeholder="you@college.edu"
                  autoComplete="username"
                  required
                />
              </div>
              <div>
                <label className="text-xs text-vault-white/60 mb-1 block">Password</label>
                <Input
                  type="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  autoComplete="current-password"
                  required
                />
              </div>
              {error && <div className="text-vault-red text-sm">{error}</div>}
              <Button type="submit" className="w-full" disabled={busy}>
                {busy ? "Signing in…" : "Sign in"}
              </Button>
            </form>
          )}
        </div>
      </div>
    </div>
  );
}
