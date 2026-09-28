export type NodeKind = "rail" | "contact" | "ncontact" | "or" | "coil" | "ton";

export interface LadderNode {
  id: string;
  kind: NodeKind;
  variable?: string;
  delayMs?: number;
  label?: string;
  x?: number;
  y?: number;
}

export interface LadderEdge {
  id: string;
  source: string;
  target: string;
}

export interface Program {
  version: number;
  name: string;
  nodes: LadderNode[];
  edges: LadderEdge[];
}

export interface TimerState {
  elapsedMs: number;
  q: boolean;
}

export interface ScanReport {
  scan: number;
  programVersion: number;
  inputs: Record<string, boolean>;
  internals: Record<string, boolean>;
  outputs: Record<string, boolean>;
  timers: Record<string, TimerState>;
}

export interface Status {
  running: boolean;
  tickMs: number;
  programVersion: number;
  programName: string;
  report: ScanReport;
}

export interface ValidateError {
  message: string;
  nodeId?: string;
  edgeId?: string;
}

export interface LoadResponse {
  loaded: boolean;
  errors?: ValidateError[];
  program?: Program;
}
