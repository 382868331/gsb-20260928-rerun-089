import type { ControllerState, Program } from './types';

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
  program: () => request<Program>('/api/program'),
  state: () => request<ControllerState>('/api/state'),
  start: () => request<ControllerState>('/api/start', { method: 'POST' }),
  pause: () => request<ControllerState>('/api/pause', { method: 'POST' }),
  stop: () => request<ControllerState>('/api/stop', { method: 'POST' }),
  scan: (count: number) => request<ControllerState>('/api/scans', { method: 'POST', body: JSON.stringify({ count }) }),
  setInput: (id: string, value: boolean) => request<ControllerState>(`/api/inputs/${id}`, { method: 'PUT', body: JSON.stringify({ value }) }),
  load: (program: Program) => request<ControllerState>('/api/program', { method: 'PUT', body: JSON.stringify(program) }),
};
