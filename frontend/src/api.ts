import type { LoadResponse, Program, Status } from "./types";

async function req<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, {
    headers: { "Content-Type": "application/json" },
    ...init,
  });
  return (await res.json()) as T;
}

export const api = {
  status: () => req<Status>("/api/status"),
  program: () => req<Program>("/api/program"),
  load: (p: Program) =>
    req<LoadResponse>("/api/program", { method: "POST", body: JSON.stringify(p) }),
  validate: (p: Program) =>
    req<LoadResponse>("/api/program/validate", {
      method: "POST",
      body: JSON.stringify(p),
    }),
  demo: () => req<LoadResponse>("/api/program/demo", { method: "POST" }),
  run: () => req<Status>("/api/run", { method: "POST" }),
  pause: () => req<Status>("/api/pause", { method: "POST" }),
  step: () => req<Status>("/api/step", { method: "POST" }),
  setInput: (name: string, value: boolean) =>
    req<Status>("/api/inputs", {
      method: "POST",
      body: JSON.stringify({ name, value }),
    }),
};
