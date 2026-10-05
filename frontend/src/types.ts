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
  compareTarget: string;
  outputs: string[];
  internals: string[];
  internalState: Record<string, boolean>;
  timerState: Record<string, { elapsedMs: number; q: boolean }>;
  message: string;
}

export type ComparisonStatus = 'diverged' | 'consistent' | 'missing';

export interface Comparison {
  compareVersion: number;
  status: ComparisonStatus;
  compareTarget: string;
  programVersion: number;
  inputVersion: number;
  parentInputVersion?: number;
  fromScan: number;
  toScan: number;
  comparedScans: number;
  missingScans: number[];
  divergence?: Divergence | null;
  message: string;
}

export type RowSource = 'original' | 'parent' | 'self';

export interface RowOrigin {
  scan: number;
  source: RowSource;
  sourceBranchId?: string;
  sourceBranchName?: string;
  inputs: Record<string, boolean>;
  note?: string;
}

export type BranchStatus = 'created' | 'applied' | 'abandoned';

export interface Branch {
  id: string;
  name: string;
  boundaryId: string;
  parentId?: string;
  parentName?: string;
  forkScan?: number;
  baseScan: number;
  baseVersion: number;
  inputVersion: number;
  status: BranchStatus;
  programName: string;
  program: Program;
  baseInputs: Record<string, boolean>;
  segment: Array<Record<string, boolean>>;
  origins: RowOrigin[];
  records: ScanRecord[];
  restarts: Array<{ scan: number; message: string }>;
  overlapNotes: string[];
  divergence?: Divergence | null;
  baseline: ScanRecord[];
  baselineInputVersion?: number;
  compareVersion: number;
  comparison?: Comparison | null;
  reports: Comparison[];
}

export interface ScanInterval {
  fromScan: number;
  toScan: number;
}

export interface ReviewScan {
  scan: number;
  status: 'compared' | 'missing';
  candidate: ScanRecord;
  baseline?: ScanRecord | null;
  outputDifferences: string[];
  internalDifferences: string[];
  firstOutputDifference: boolean;
  timerState: Record<string, { elapsedMs: number; q: boolean }>;
}

export interface ReviewNote {
  id: string;
  compareVersion: number;
  scan?: number;
  text: string;
  createdAt: string;
  updatedAt: string;
}

export interface ReviewPackage {
  kind: string;
  formatVersion: number;
  id: string;
  originalId?: string;
  branchId: string;
  branchName: string;
  branchStatus: BranchStatus;
  parentId?: string;
  parentName?: string;
  baseScan: number;
  forkScan?: number;
  programVersion: number;
  inputVersion: number;
  program: Program;
  exportedAt: string;
  currentReport: Comparison;
  historicalReports: Comparison[];
  comparedScans: ReviewScan[];
  missingScans: ReviewScan[];
  missingIntervals: ScanInterval[];
  bindingDescription: string;
  notes: ReviewNote[];
}
