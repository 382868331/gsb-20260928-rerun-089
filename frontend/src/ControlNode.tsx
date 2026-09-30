import { Handle, Position, useReactFlow, type NodeProps } from '@xyflow/react';
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

export type ControlNodeData = {
  label: string;
  kind: NodeKind;
  presetMs?: number;
  active?: boolean;
};

export function ControlNodeView({ id, data }: NodeProps) {
  const node = data as unknown as ControlNodeData;
  const { setNodes } = useReactFlow();
  const inputPorts = inputsByKind[node.kind] ?? [];

  const rename = () => {
    const label = window.prompt('元件名称', node.label);
    if (label?.trim()) {
      setNodes((current) => current.map((item) => (
        item.id === id ? { ...item, data: { ...item.data, label: label.trim() } } : item
      )));
    }
  };

  const changePreset = () => {
    if (node.kind !== 'TON') return;
    const raw = window.prompt('延时预设（毫秒，必须大于 0）', String(node.presetMs ?? 1000));
    const presetMs = Number(raw);
    if (raw !== null && Number.isInteger(presetMs) && presetMs > 0) {
      setNodes((current) => current.map((item) => (
        item.id === id ? { ...item, data: { ...item.data, presetMs } } : item
      )));
    }
  };

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
      <div className="node-head">
        <span className="node-kind" style={{ background: kindColor[node.kind] }}>{node.kind}</span>
        <button className="node-edit" title="双击节点可改名" onClick={rename}>改名</button>
      </div>
      <div className="node-label" onDoubleClick={rename} title="双击改名">{node.label}</div>
      {node.kind === 'TON' ? (
        <button className="node-meta" onClick={changePreset}>预设 {node.presetMs ?? 0}ms（点击修改）</button>
      ) : null}
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
