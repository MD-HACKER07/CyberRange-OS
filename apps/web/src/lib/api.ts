// Browser API client. Access tokens live in memory only; the refresh token is
// an httpOnly cookie set by the API. On a 401 we transparently refresh once.
"use client";

let accessToken: string | null = null;
let refreshPromise: Promise<boolean> | null = null;

export function setAccessToken(token: string | null) {
  accessToken = token;
}
export function getAccessToken() {
  return accessToken;
}

const BASE = "/api";

async function refresh(): Promise<boolean> {
  if (!refreshPromise) {
    refreshPromise = fetch(`${BASE}/auth/refresh`, {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
    })
      .then(async (res) => {
        if (!res.ok) return false;
        const data = await res.json();
        accessToken = data.access_token;
        return true;
      })
      .catch(() => false)
      .finally(() => {
        refreshPromise = null;
      });
  }
  return refreshPromise;
}

export class APIError extends Error {
  status: number;
  code?: string;
  constructor(status: number, message: string, code?: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

interface RequestOpts {
  method?: string;
  body?: unknown;
  raw?: boolean;
  retry?: boolean;
}

export async function api<T = unknown>(path: string, opts: RequestOpts = {}): Promise<T> {
  const headers: Record<string, string> = {};
  if (opts.body !== undefined) headers["Content-Type"] = "application/json";
  if (accessToken) headers["Authorization"] = `Bearer ${accessToken}`;

  const res = await fetch(`${BASE}${path}`, {
    method: opts.method || (opts.body !== undefined ? "POST" : "GET"),
    credentials: "include",
    headers,
    body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
  });

  if (res.status === 401 && opts.retry !== false) {
    const ok = await refresh();
    if (ok) return api<T>(path, { ...opts, retry: false });
  }

  if (!res.ok) {
    let message = res.statusText;
    let code: string | undefined;
    try {
      const err = await res.json();
      message = err.error || message;
      code = err.code;
    } catch {
      /* ignore */
    }
    throw new APIError(res.status, message, code);
  }

  if (res.status === 204) return undefined as T;
  if (opts.raw) return (await res.blob()) as unknown as T;
  const ct = res.headers.get("content-type") || "";
  if (ct.includes("application/json")) return res.json();
  return (await res.text()) as unknown as T;
}

export async function login(loginId: string, password: string) {
  const data = await api<{ access_token: string; user: unknown }>("/auth/login", {
    body: { login: loginId, password },
    retry: false,
  });
  accessToken = data.access_token;
  return data;
}

export async function tryRestoreSession(): Promise<boolean> {
  return refresh();
}

export async function logout() {
  try {
    await api("/auth/logout", { method: "POST" });
  } finally {
    accessToken = null;
  }
}

// SWR fetcher
export const fetcher = <T>(path: string) => api<T>(path);

// Build a WebSocket URL carrying the access token (browsers can't set WS headers).
export function wsURL(path: string): string {
  const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
  const token = accessToken ? `?access_token=${encodeURIComponent(accessToken)}` : "";
  return `${proto}//${window.location.host}${path}${token}`;
}
