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
  program: Program;
  scanNumber: number;
  scanMs: number;
  inputs: Record<string, boolean>;
  internals: Record<string, unknown>;
  outputs: Record<string, boolean>;
  timers: Record<string, { elapsedMs: number; q: boolean }>;
  log: Array<{ scan: number; message: string }>;
  boundaries?: Boundary[];
  branches?: Branch[];
}

export interface ProgramDiff {
  compatible: boolean;
  paused: boolean;
  timerChanges: string[];
  renamedInputs: string[];
  removedNodes: string[];
  addedNodes: string[];
  messages: string[];
}

export interface ScanRecord {
  scan: number;
  version: number;
  inputs: Record<string, boolean>;
  outputs: Record<string, boolean>;
  internals: Record<string, boolean>;
  timers: Record<string, { elapsedMs: number; q: boolean }>;
}

export interface Boundary {
  id: string;
  label: string;
  scan: number;
  version: number;
  program: Program;
}

export interface Divergence {
  scan: number;
  outputs: string[];
  internals: string[];
  message: string;
}

export interface Branch {
  id: string;
  name: string;
  boundaryId: string;
  baseScan: number;
  baseVersion: number;
  programName: string;
  program: Program;
  baseInputs: Record<string, boolean>;
  segment: Array<Record<string, boolean>>;
  records: ScanRecord[];
  restarts: Array<{ scan: number; message: string }>;
  divergence?: Divergence | null;
}
