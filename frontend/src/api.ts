import type { ControllerState, Program, ProgramDiff } from './types';

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
  load: (program: Program, mode: 'restart' | 'preserve' = 'restart') =>
    request<ControllerState>(`/api/program?mode=${mode}`, { method: 'PUT', body: JSON.stringify(program) }),
  preview: (program: Program) => request<ProgramDiff>('/api/program/preview', { method: 'POST', body: JSON.stringify(program) }),
  saveBoundary: (label: string) => request<ControllerState>('/api/boundaries', { method: 'POST', body: JSON.stringify({ label }) }),
  createBranch: (boundaryId: string, name: string) =>
    request<ControllerState>('/api/branches', { method: 'POST', body: JSON.stringify({ boundaryId, name }) }),
  createChildBranch: (parentId: string, atScan: number, name: string) =>
    request<ControllerState>(`/api/branches/${parentId}/child`, { method: 'POST', body: JSON.stringify({ parentId, atScan, name }) }),
  replaceSegment: (id: string, fromScan: number, inputs: Array<Record<string, boolean>>) =>
    request<ControllerState>(`/api/branches/${id}/segment`, { method: 'PUT', body: JSON.stringify({ fromScan, inputs }) }),
  restartElement: (id: string, nodeId: string) =>
    request<ControllerState>(`/api/branches/${id}/restart`, { method: 'POST', body: JSON.stringify({ nodeId }) }),
  abandonBranch: (id: string) => request<ControllerState>(`/api/branches/${id}`, { method: 'DELETE' }),
};
