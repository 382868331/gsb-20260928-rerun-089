import { Handle, Position, type NodeProps } from "@xyflow/react";
import type { LadderNode, ScanReport } from "./types";

export interface LadderNodeData {
  ladder: LadderNode;
  report?: ScanReport;
}

const kindTitle: Record<string, string> = {
  rail: "左电源轨",
  contact: "常开触点",
  ncontact: "常闭触点",
  or: "分支汇合 OR",
  coil: "线圈",
  ton: "通电延时 TON",
};

function variableState(ladder: LadderNode, report?: ScanReport): boolean {
  const name = ladder.variable ?? "";
  if (!report) return false;
  if (name.startsWith("in.")) return !!report.inputs[name];
  if (name.startsWith("m.")) return !!report.internals[name];
  if (name.startsWith("out.")) return !!report.outputs[name];
  if (name.startsWith("t.")) return !!report.timers[name]?.q;
  return false;
}

export function LadderNodeView({ data }: NodeProps) {
  const { ladder, report } = data as unknown as LadderNodeData;
  const on = variableState(ladder, report);
  const terminal = ladder.kind === "coil" || ladder.kind === "ton";
  const timer = ladder.variable ? report?.timers[ladder.variable] : undefined;

  return (
    <div className={`lad-node kind-${ladder.kind} ${on ? "energized" : ""}`}>
      {ladder.kind !== "rail" && (
        <Handle type="target" position={Position.Left} isConnectable />
      )}
      <div className="lad-kind">{kindTitle[ladder.kind]}</div>
      {ladder.variable && <div className="lad-var">{ladder.variable}</div>}
      {ladder.kind === "ton" && (
        <div className="lad-meta">
          设定 {ladder.delayMs} ms
          {timer && (
            <span className={timer.q ? "on" : ""}>
              {" "}
              · 累计 {timer.elapsedMs} ms {timer.q ? "· Q=1" : "· Q=0"}
            </span>
          )}
        </div>
      )}
      {!terminal && ladder.kind !== "rail" && (
        <Handle type="source" position={Position.Right} isConnectable />
      )}
      {ladder.kind === "rail" && (
        <Handle type="source" position={Position.Right} isConnectable />
      )}
    </div>
  );
}

export const nodeTypes = { ladder: LadderNodeView };
