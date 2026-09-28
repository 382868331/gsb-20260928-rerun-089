import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Background,
  Controls,
  ReactFlow,
  addEdge,
  applyEdgeChanges,
  applyNodeChanges,
  useEdgesState,
  useNodesState,
  type Connection,
  type Edge,
  type Node,
  type NodeChange,
  type EdgeChange,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import { Play, Pause, Square, StepForward, Upload, RotateCcw } from 'lucide-react';
import { api } from './api';
import { demoProgram } from './demo';
import { nodeTypes } from './ControlNode';
import type { ControllerState, Program } from './types';
import './style.css';

const initialFlow = toFlow(demoProgram, null);

function toFlow(program: Program, state: ControllerState | null): { nodes: Node[]; edges: Edge[] } {
  return {
    nodes: program.nodes.map((item) => ({
      id: item.id,
      type: 'control',
      position: item.position,
      data: {
        kind: item.kind,
        label: item.label,
        presetMs: item.presetMs,
        active: Boolean(state?.inputs[item.id] || state?.internals[item.id] || state?.outputs[item.id]),
      },
    })),
    edges: program.edges.map((item) => ({
      id: item.id,
      source: item.source,
      sourceHandle: item.sourcePort,
      target: item.target,
      targetHandle: item.targetPort,
      animated: Boolean(state?.outputs[item.target]),
    })),
  };
}

function fromFlow(nodes: Node[], edges: Edge[]): Program {
  const ordered = demoProgram;
  return {
    name: ordered.name,
    nodes: nodes.map((node) => {
      const source = ordered.nodes.find((item) => item.id === node.id);
      return {
        id: node.id,
        kind: source?.kind ?? 'INPUT',
        label: String(node.data.label),
        position: { x: node.position.x, y: node.position.y },
        presetMs: typeof node.data.presetMs === 'number' ? node.data.presetMs : undefined,
      };
    }),
    edges: edges.map((edge) => ({
      id: edge.id,
      source: edge.source,
      sourcePort: edge.sourceHandle ?? 'q',
      target: edge.target,
      targetPort: edge.targetHandle ?? 'in',
    })),
  };
}

export default function App() {
  const [state, setState] = useState<ControllerState | null>(null);
  const [error, setError] = useState('');
  const [nodes, setNodes] = useNodesState<Node>(initialFlow.nodes);
  const [edges, setEdges] = useEdgesState<Edge>(initialFlow.edges);

  const refresh = useCallback(async () => {
    try {
      const next = await api.state();
      setState(next);
      setError('');
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh]);

  useEffect(() => {
    if (state?.mode !== 'running') return;
    const timer = window.setInterval(refresh, 100);
    return () => window.clearInterval(timer);
  }, [state?.mode, refresh]);

  useEffect(() => {
    setNodes((current) => current.map((node) => ({
      ...node,
      data: { ...node.data, active: Boolean(state?.inputs[node.id] || state?.internals[node.id] || state?.outputs[node.id]) },
    })));
    setEdges((current) => current.map((edge) => ({ ...edge, animated: Boolean(state?.outputs[edge.target]) })));
  }, [state, setNodes, setEdges]);

  const runAction = async (action: () => Promise<ControllerState>) => {
    try {
      setState(await action());
      setError('');
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const onConnect = useCallback((connection: Connection) => {
    setEdges((current) => addEdge({ ...connection, id: `edge-${Date.now()}` }, current));
  }, [setEdges]);

  const onNodesChangeWrapped = useCallback((changes: NodeChange<Node>[]) => {
    setNodes((current) => applyNodeChanges(changes, current));
  }, [setNodes]);

  const onEdgesChangeWrapped = useCallback((changes: EdgeChange<Edge>[]) => {
    setEdges((current) => applyEdgeChanges(changes, current));
  }, [setEdges]);

  const loadEditedProgram = async () => {
    await runAction(() => api.load(fromFlow(nodes, edges)));
  };

  const resetDemo = async () => {
    const flow = toFlow(demoProgram, state);
    setNodes(flow.nodes);
    setEdges(flow.edges);
    await runAction(() => api.load(demoProgram));
  };

  const inputNodes = useMemo(() => demoProgram.nodes.filter((node) => node.kind === 'INPUT'), []);

  return (
    <main>
      <header>
        <div>
          <h1>工业控制虚拟控制器</h1>
          <p>显式扫描顺序 · SR 保持 · TON 通电延时 · 仅仿真 I/O</p>
        </div>
        <div className="status">
          <span className={`mode ${state?.mode ?? 'stopped'}`}>{state?.mode ?? 'loading'}</span>
          <span>版本 {state?.version ?? '-'}</span>
          <span>扫描 {state?.scanNumber ?? 0}</span>
          <span>{state?.scanMs ?? 100}ms / 次</span>
        </div>
      </header>

      <section className="toolbar">
        <button onClick={() => runAction(api.start)}><Play size={16} />连续运行</button>
        <button onClick={() => runAction(api.pause)}><Pause size={16} />暂停</button>
        <button onClick={() => runAction(api.stop)}><Square size={16} />停止复位</button>
        <button onClick={() => runAction(() => api.scan(1))}><StepForward size={16} />单次扫描</button>
        <button onClick={() => runAction(() => api.scan(5))}><StepForward size={16} />扫描 5 次</button>
        <button onClick={loadEditedProgram}><Upload size={16} />载入编辑版本</button>
        <button onClick={resetDemo}><RotateCcw size={16} />恢复示例</button>
      </section>

      {error && <div className="error">{error}</div>}

      <section className="workspace">
        <div className="canvas">
          <ReactFlow
            nodes={nodes}
            edges={edges}
            nodeTypes={nodeTypes}
            onNodesChange={onNodesChangeWrapped}
            onEdgesChange={onEdgesChangeWrapped}
            onConnect={onConnect}
            fitView
            deleteKeyCode={['Backspace', 'Delete']}
          >
            <Background />
            <Controls />
          </ReactFlow>
        </div>

        <aside>
          <h2>采样输入</h2>
          <div className="inputs">
            {inputNodes.map((node) => (
              <label key={node.id}>
                <span>{node.label}</span>
                <button
                  className={state?.inputs[node.id] ? 'on' : 'off'}
                  onMouseDown={() => runAction(() => api.setInput(node.id, true))}
                  onMouseUp={() => runAction(() => api.setInput(node.id, false))}
                  onMouseLeave={() => state?.inputs[node.id] && runAction(() => api.setInput(node.id, false))}
                >按住接通</button>
              </label>
            ))}
          </div>

          <h2>真实扫描输出</h2>
          <div className="cards">
            {Object.entries(state?.outputs ?? {}).map(([id, value]) => (
              <div key={id} className={`card ${value ? 'on' : 'off'}`}>
                <strong>{demoProgram.nodes.find((node) => node.id === id)?.label ?? id}</strong>
                <span>{value ? 'ON' : 'OFF'}</span>
              </div>
            ))}
          </div>

          <h2>内部状态</h2>
          <div className="timers">
            {Object.entries(state?.timers ?? {}).map(([id, timer]) => (
              <div key={id}>
                <span>{demoProgram.nodes.find((node) => node.id === id)?.label ?? id}</span>
                <progress max={500} value={timer.elapsedMs} />
                <em>{timer.elapsedMs}ms / Q={timer.q ? '1' : '0'}</em>
              </div>
            ))}
          </div>

          <h2>扫描日志</h2>
          <ol className="log">
            {(state?.log ?? []).slice().reverse().map((event, index) => (
              <li key={`${event.scan}-${index}`}><b>#{event.scan}</b>{event.message}</li>
            ))}
          </ol>
        </aside>
      </section>
    </main>
  );
}
