"use client";

import { Shell } from "@/components/shell";
import { useRequireAuth } from "@/lib/auth-context";
import { Spinner } from "@/components/ui";

export default function AppLayout({ children }: { children: React.ReactNode }) {
  const { user, loading } = useRequireAuth();
  if (loading || !user) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <Spinner label="Loading…" />
      </div>
    );
  }
  return <Shell>{children}</Shell>;
}
