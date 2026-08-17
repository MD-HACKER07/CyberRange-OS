"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { tryRestoreSession } from "@/lib/api";
import { useAuth } from "@/lib/auth-context";
import { Spinner } from "@/components/ui";

export default function SSOComplete() {
  const router = useRouter();
  const { refresh } = useAuth();
  useEffect(() => {
    (async () => {
      await tryRestoreSession();
      await refresh();
      router.replace("/dashboard");
    })();
  }, [router, refresh]);
  return (
    <div className="min-h-screen flex items-center justify-center">
      <Spinner label="Completing sign-in…" />
    </div>
  );
}
