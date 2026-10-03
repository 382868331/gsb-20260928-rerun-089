import type { Branch, Boundary, ControllerState, Program, ProgramDiff } from './types';

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
  forkBranch: (parentId: string, forkScan: number, name: string) =>
    request<ControllerState>(`/api/branches/${parentId}/fork`, { method: 'POST', body: JSON.stringify({ forkScan, name }) }),
  abandonBranch: (id: string) =>
    request<ControllerState>(`/api/branches/${id}/abandon`, { method: 'POST' }),
  rebaseComparison: (id: string) =>
    request<ControllerState>(`/api/branches/${id}/rebase`, { method: 'POST' }),
  findTrials: (params: { programVersion?: number; inputVersion?: number; scan?: number }) => {
    const query = new URLSearchParams();
    if (params.programVersion) query.set('programVersion', String(params.programVersion));
    if (params.inputVersion) query.set('inputVersion', String(params.inputVersion));
    if (params.scan) query.set('scan', String(params.scan));
    return request<{ boundaries: Boundary[]; branches: Branch[] }>(`/api/trials?${query.toString()}`);
  },
  replaceSegment: (id: string, fromScan: number, inputs: Array<Record<string, boolean>>) =>
    request<ControllerState>(`/api/branches/${id}/segment`, { method: 'PUT', body: JSON.stringify({ fromScan, inputs }) }),
  restartElement: (id: string, nodeId: string) =>
    request<ControllerState>(`/api/branches/${id}/restart`, { method: 'POST', body: JSON.stringify({ nodeId }) }),
};
