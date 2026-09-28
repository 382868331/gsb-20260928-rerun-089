export type NodeKind = 'INPUT' | 'OUTPUT' | 'AND' | 'OR' | 'NOT' | 'SR' | 'TON';

export interface ControlNode {
  id: string;
  kind: NodeKind;
  label: string;
  position: { x: number; y: number };
  presetMs?: number;
}

export interface ControlEdge {
  id: string;
  source: string;
  sourcePort: string;
  target: string;
  targetPort: string;
}

export interface Program {
  name: string;
  nodes: ControlNode[];
  edges: ControlEdge[];
}

export interface ControllerState {
  mode: 'stopped' | 'paused' | 'running';
  version: number;
  programName: string;
  scanNumber: number;
  scanMs: number;
  inputs: Record<string, boolean>;
  internals: Record<string, unknown>;
  outputs: Record<string, boolean>;
  timers: Record<string, { elapsedMs: number; q: boolean }>;
  log: Array<{ scan: number; message: string }>;
}
