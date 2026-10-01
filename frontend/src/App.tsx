import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
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
import type { ControllerState, Program, RevisionImpact } from './types';
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

function fromFlow(name: string, nodes: Node[], edges: Edge[]): Program {
  return {
    name,
    nodes: nodes.map((node) => ({
      id: node.id,
      kind: (node.data.kind as Program['nodes'][number]['kind']) ?? 'INPUT',
      label: String(node.data.label),
      position: { x: node.position.x, y: node.position.y },
      presetMs: typeof node.data.presetMs === 'number' ? node.data.presetMs : undefined,
    })),
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
  const [impact, setImpact] = useState<RevisionImpact | null>(null);
  const [pendingProgram, setPendingProgram] = useState<Program | null>(null);
  const versionRef = useRef<number | null>(null);

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
    if (!state || versionRef.current === state.version) return;
    versionRef.current = state.version;
    const flow = toFlow(state.program, state);
    setNodes(flow.nodes);
    setEdges(flow.edges);
  }, [state, setNodes, setEdges]);

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
    try {
      const program = fromFlow(state?.programName ?? '编辑版本', nodes, edges);
      const preview = await api.preview(program);
      setPendingProgram(program);
      setImpact(preview);
      setError('');
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const applyLoad = async (mode: 'merge' | 'restart') => {
    if (!pendingProgram) return;
    await runAction(() => api.load(pendingProgram, mode));
    setImpact(null);
    setPendingProgram(null);
  };

  const resetDemo = async () => {
    await runAction(() => api.load(demoProgram, 'restart'));
  };

  const program = state?.program ?? null;
  const inputNodes = useMemo(() => (program?.nodes ?? []).filter((node) => node.kind === 'INPUT'), [program]);
  const labelOf = useCallback((id: string) => program?.nodes.find((node) => node.id === id)?.label ?? id, [program]);
  const presetOf = useCallback((id: string) => program?.nodes.find((node) => node.id === id)?.presetMs ?? 500, [program]);

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
          <span>{state?.programName ?? ''}</span>
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

      {impact && (
        <section className="impact">
          <h2>版本影响预览（尚未生效）</h2>
          {!impact.compatible && <p className="warn">存在类型变更，部分状态无法保留。</p>}
          <ul>
            {impact.timerChanges.map((item) => <li key={`t-${item.id}`}><b>互锁延时</b> {item.label}：{item.detail}</li>)}
            {impact.renamedInputs.map((item) => <li key={`r-${item.id}`}><b>输入重命名</b>：{item.detail}</li>)}
            {impact.removedNodes.map((item) => <li key={`d-${item.id}`}><b>元件删除</b> {item.label}：{item.detail}</li>)}
            {impact.addedNodes.map((item) => <li key={`a-${item.id}`}><b>新增元件</b> {item.label}：{item.detail}</li>)}
            {!impact.timerChanges.length && !impact.renamedInputs.length && !impact.removedNodes.length && !impact.addedNodes.length && (
              <li>与当前版本完全兼容，无状态影响。</li>
            )}
          </ul>
          <div className="impact-actions">
            <button onClick={() => applyLoad('merge')}>保留可兼容状态载入</button>
            <button onClick={() => applyLoad('restart')}>明确重新开始</button>
            <button onClick={() => { setImpact(null); setPendingProgram(null); }}>取消</button>
          </div>
        </section>
      )}

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
                <strong>{labelOf(id)}</strong>
                <span>{value ? 'ON' : 'OFF'}</span>
              </div>
            ))}
          </div>

          <h2>内部状态</h2>
          <div className="timers">
            {Object.entries(state?.timers ?? {}).map(([id, timer]) => (
              <div key={id}>
                <span>{labelOf(id)}</span>
                <progress max={presetOf(id)} value={timer.elapsedMs} />
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
