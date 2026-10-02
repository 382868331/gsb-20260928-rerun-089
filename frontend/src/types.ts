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
  boundaries: BoundaryInfo[];
  branches: BranchInfo[];
}

export interface BoundaryInfo {
  id: string;
  name: string;
  scan: number;
  version: number;
}

export interface InputOverride {
  scan: number;
  input: string;
  value: boolean;
}

export interface BranchInfo {
  id: string;
  name: string;
  boundaryId: string;
  originScan: number;
  version: number;
  scan: number;
  program: Program;
  inputs: Record<string, boolean>;
  overrides: InputOverride[];
  outputs: Record<string, boolean>;
  timers: Record<string, { elapsedMs: number; q: boolean }>;
  resetLog: Array<{ scan: number; message: string }>;
}

export interface Divergence {
  found: boolean;
  scan: number;
  outputs: string[];
  internals: string[];
  comparedScans: number;
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
