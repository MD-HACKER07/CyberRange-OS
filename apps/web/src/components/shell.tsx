"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { ReactNode } from "react";
import {
  ShieldCheck,
  LayoutDashboard,
  Swords,
  Radar,
  FileText,
  Trophy,
  GraduationCap,
  Settings,
  Brain,
  Crosshair,
  LogOut,
  BarChart3,
} from "lucide-react";
import { useAuth } from "@/lib/auth-context";
import { cn } from "@/lib/cn";
import type { Role } from "@/lib/types";

interface NavItem {
  href: string;
  label: string;
  icon: ReactNode;
  roles?: Role[];
  accent?: "gold" | "red";
}

const NAV: NavItem[] = [
  { href: "/dashboard", label: "Dashboard", icon: <LayoutDashboard size={18} /> },
  { href: "/red-team", label: "Red Team", icon: <Swords size={18} />, roles: ["student", "faculty", "admin"], accent: "red" },
  { href: "/blue-team", label: "Blue Team", icon: <Radar size={18} />, roles: ["student", "faculty", "admin"] },
  { href: "/reports", label: "Reports", icon: <FileText size={18} /> },
  { href: "/leaderboard", label: "Leaderboard", icon: <Trophy size={18} /> },
  { href: "/learning-paths", label: "Learning Paths", icon: <GraduationCap size={18} />, roles: ["student", "faculty", "admin"] },
  { href: "/faculty", label: "Faculty", icon: <BarChart3 size={18} />, roles: ["faculty", "admin", "auditor"] },
  { href: "/ai-security", label: "AI Security", icon: <Brain size={18} />, roles: ["faculty", "admin"] },
  { href: "/attack", label: "ATT&CK", icon: <Crosshair size={18} /> },
  { href: "/admin", label: "Admin", icon: <Settings size={18} />, roles: ["admin"] },
];

export function Shell({ children }: { children: ReactNode }) {
  const pathname = usePathname();
  const { user, signOut } = useAuth();
  if (!user) return null;

  const items = NAV.filter((n) => !n.roles || n.roles.includes(user.role));

  return (
    <div className="min-h-screen flex">
      <aside className="w-60 shrink-0 border-r border-vault-slate bg-black-soft flex flex-col">
        <div className="p-4 flex items-center gap-2 border-b border-vault-slate">
          <ShieldCheck className="text-vault-gold" size={20} />
          <span className="font-display text-vault-gold tracking-widest text-xs uppercase">CyberRange OS</span>
        </div>
        <nav className="flex-1 p-2 space-y-1 overflow-auto">
          {items.map((item) => {
            const active = pathname === item.href || pathname.startsWith(item.href + "/");
            return (
              <Link
                key={item.href}
                href={item.href}
                className={cn(
                  "flex items-center gap-3 px-3 py-2 rounded-md text-sm transition-colors",
                  active
                    ? item.accent === "red"
                      ? "bg-vault-red/15 text-vault-red"
                      : "bg-vault-gold/15 text-vault-gold"
                    : "text-vault-white/70 hover:bg-white/5",
                )}
              >
                {item.icon}
                {item.label}
              </Link>
            );
          })}
        </nav>
        <div className="p-3 border-t border-vault-slate">
          <div className="text-sm mb-1">{user.name}</div>
          <div className="text-xs text-vault-white/40 mb-3 uppercase tracking-wide">{user.role}</div>
          <button
            onClick={signOut}
            className="flex items-center gap-2 text-xs text-vault-white/60 hover:text-vault-red"
          >
            <LogOut size={14} /> Sign out
          </button>
        </div>
      </aside>
      <main className="flex-1 overflow-auto">{children}</main>
    </div>
  );
}
