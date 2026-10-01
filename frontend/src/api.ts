import type { ControllerState, Program, RevisionImpact } from './types';

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const response = await fetch(url, {
    headers: { 'Content-Type': 'application/json' },
    ...init,
  });
  if (!response.ok) {
    throw new Error(await response.text());
  }
  return response.json() as Promise<T>;
}

export const api = {
  state: () => request<ControllerState>('/api/state'),
  start: () => request<ControllerState>('/api/start', { method: 'POST' }),
  pause: () => request<ControllerState>('/api/pause', { method: 'POST' }),
  stop: () => request<ControllerState>('/api/stop', { method: 'POST' }),
  scan: (count: number) => request<ControllerState>('/api/scans', { method: 'POST', body: JSON.stringify({ count }) }),
  setInput: (id: string, value: boolean) => request<ControllerState>(`/api/inputs/${id}`, { method: 'PUT', body: JSON.stringify({ value }) }),
  preview: (program: Program) => request<RevisionImpact>('/api/program?mode=preview', { method: 'PUT', body: JSON.stringify(program) }),
  load: (program: Program, mode: 'merge' | 'restart' = 'restart') =>
    request<ControllerState>(`/api/program?mode=${mode}`, { method: 'PUT', body: JSON.stringify(program) }),
};
