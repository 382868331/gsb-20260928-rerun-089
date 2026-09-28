import { Handle, Position, type NodeProps } from '@xyflow/react';
import type { NodeKind } from './types';

const inputsByKind: Partial<Record<NodeKind, string[]>> = {
  AND: ['a', 'b'],
  OR: ['a', 'b'],
  NOT: ['in'],
  SR: ['set', 'reset'],
  TON: ['in'],
  OUTPUT: ['in'],
};

const kindColor: Record<NodeKind, string> = {
  INPUT: '#2563eb',
  OUTPUT: '#dc2626',
  AND: '#7c3aed',
  OR: '#7c3aed',
  NOT: '#7c3aed',
  SR: '#0891b2',
  TON: '#ca8a04',
};

type NodeData = {
  label: string;
  kind: NodeKind;
  presetMs?: number;
  active?: boolean;
};

export function ControlNodeView({ data }: NodeProps) {
  const node = data as unknown as NodeData;
  const inputPorts = inputsByKind[node.kind] ?? [];
  return (
    <div className={`control-node ${node.active ? 'active' : ''}`} style={{ borderColor: kindColor[node.kind] }}>
      {inputPorts.map((port, index) => (
        <Handle
          key={port}
          type="target"
          position={Position.Left}
          id={port}
          className="port"
          style={{ top: `${((index + 1) / (inputPorts.length + 1)) * 100}%` }}
        />
      ))}
      <div className="node-kind" style={{ background: kindColor[node.kind] }}>{node.kind}</div>
      <div className="node-label">{node.label}</div>
      {node.presetMs ? <div className="node-meta">预设 {node.presetMs}ms</div> : null}
      {inputPorts.map((port) => <span key={port} className={`port-label left ${port}`}>{port}</span>)}
      {node.kind !== 'OUTPUT' && (
        <>
          <span className="port-label right">q</span>
          <Handle type="source" position={Position.Right} id="q" className="port" />
        </>
      )}
    </div>
  );
}

export const nodeTypes = { control: ControlNodeView };
