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
import { BranchPanel } from './BranchPanel';
import type { ControllerState, Program, ProgramDiff } from './types';
import './style.css';

const initialFlow = toFlow(demoProgram, null);

function toFlow(program: Program, state: ControllerState | null): { nodes: Node[]; edges: Edge[] } {
  return {
    nodes: program.nodes.map((item) => ({
      id: item.id,
      type: 'control',
      // 服务端对空 position 会省略字段，缺省坐标避免画布因空值整体空白。
      position: item.position ?? { x: 0, y: 0 },
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
      position: { x: Math.round(node.position.x), y: Math.round(node.position.y) },
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
  const [dirty, setDirty] = useState(false);
  const [preview, setPreview] = useState<{ program: Program; diff: ProgramDiff } | null>(null);
  const [loadError, setLoadError] = useState('');

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

  // 画布跟随服务端实际程序：版本变化且本地无未提交编辑时整体同步。
  useEffect(() => {
    if (!state || dirty || !state.program?.nodes?.length) return;
    const flow = toFlow(state.program, state);
    setNodes(flow.nodes);
    setEdges(flow.edges);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [state?.version, dirty]);

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

  const markDirty = () => setDirty(true);

  const onConnect = useCallback((connection: Connection) => {
    setDirty(true);
    setEdges((current) => addEdge({ ...connection, id: `edge-${Date.now()}` }, current));
  }, [setEdges]);

  const onNodesChangeWrapped = useCallback((changes: NodeChange<Node>[]) => {
    if (changes.some((change) => change.type !== 'select' && change.type !== 'dimensions')) setDirty(true);
    setNodes((current) => applyNodeChanges(changes, current));
  }, [setNodes]);

  const onEdgesChangeWrapped = useCallback((changes: EdgeChange<Edge>[]) => {
    if (changes.some((change) => change.type !== 'select')) setDirty(true);
    setEdges((current) => applyEdgeChanges(changes, current));
  }, [setEdges]);

  // 载入前先预览修订影响；非法连接在生效前被服务端拒绝。
  const loadEditedProgram = async () => {
    if (!state) return;
    const program = fromFlow(state.programName, nodes, edges);
    try {
      const diff = await api.preview(program);
      setPreview({ program, diff });
      setLoadError('');
      setError('');
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  // 载入失败时保留本地编辑与原暂停程序：不清 dirty、不关预览，
  // 明确提示原版本未变，避免误以为新版本已生效。
  const confirmLoad = async (mode: 'restart' | 'preserve') => {
    if (!preview) return;
    try {
      const next = await api.load(preview.program, mode);
      setState(next);
      setPreview(null);
      setLoadError('');
      setDirty(false);
      setError('');
    } catch (err) {
      setLoadError(err instanceof Error ? err.message : String(err));
    }
  };

  const resetDemo = async () => {
    setDirty(false);
    setPreview(null);
    setLoadError('');
    await runAction(() => api.load(demoProgram, 'restart'));
  };

  const program = state?.program ?? demoProgram;
  const labelOf = useMemo(() => {
    const map = new Map(program.nodes.map((node) => [node.id, node.label]));
    return (id: string) => map.get(id) ?? id;
  }, [program]);
  const inputNodes = useMemo(() => program.nodes.filter((node) => node.kind === 'INPUT'), [program]);
  const presetOf = useMemo(() => {
    const map = new Map(program.nodes.map((node) => [node.id, node.presetMs ?? 0]));
    return (id: string) => map.get(id) ?? 0;
  }, [program]);

  return (
    <main>
      <header>
        <div>
          <h1>工业控制虚拟控制器</h1>
          <p>{state?.programName ?? '...'} · 显式扫描顺序 · SR 保持 · TON 通电延时 · 仅仿真 I/O</p>
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
        <button onClick={loadEditedProgram} disabled={!dirty && !state}><Upload size={16} />载入编辑版本{dirty ? ' *' : ''}</button>
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
            onNodeDragStart={markDirty}
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
                <progress max={presetOf(id) || 500} value={timer.elapsedMs} />
                <em>{timer.elapsedMs}ms / {presetOf(id)}ms · Q={timer.q ? '1' : '0'}</em>
              </div>
            ))}
          </div>

          <h2>扫描日志</h2>
          <ol className="log">
            {(state?.log ?? []).slice().reverse().map((event, index) => (
              <li key={`${event.scan}-${index}`}><b>#{event.scan}</b>{event.message}</li>
            ))}
          </ol>

          {state && <BranchPanel state={state} onState={setState} onError={setError} />}
        </aside>
      </section>

      {preview && (
        <div className="modal-backdrop">
          <div className="modal">
            <h2>载入前预览：版本 {state?.version} → {(state?.version ?? 0) + 1}</h2>
            {preview.diff.paused && (
              <p className="modal-note">当前处于暂停状态。请选择保留可兼容状态继续，或明确重新开始（清零定时累计与保持状态）。</p>
            )}
            {preview.diff.timerChanges.length > 0 && (
              <section>
                <h3>互锁延时变化</h3>
                <ul>{preview.diff.timerChanges.map((item) => <li key={item}>{item}</li>)}</ul>
              </section>
            )}
            {preview.diff.renamedInputs.length > 0 && (
              <section>
                <h3>输入重命名</h3>
                <ul>{preview.diff.renamedInputs.map((item) => <li key={item}>{item}</li>)}</ul>
              </section>
            )}
            {preview.diff.removedNodes.length > 0 && (
              <section>
                <h3>元件删除</h3>
                <ul>{preview.diff.removedNodes.map((item) => <li key={item}>{item}</li>)}</ul>
              </section>
            )}
            {preview.diff.addedNodes.length > 0 && (
              <section>
                <h3>新增元件</h3>
                <ul>{preview.diff.addedNodes.map((item) => <li key={item}>{item}</li>)}</ul>
              </section>
            )}
            {preview.diff.messages.length > 0 && (
              <section>
                <h3>其他说明</h3>
                <ul>{preview.diff.messages.map((item) => <li key={item}>{item}</li>)}</ul>
              </section>
            )}
            {!preview.diff.compatible && (
              <p className="modal-warning">包含不兼容变更，无法保留当前状态，只能明确重新开始。</p>
            )}
            {loadError && (
              <p className="modal-warning">载入失败，原暂停程序与版本 {state?.version} 保持不变：{loadError}</p>
            )}
            <div className="modal-actions">
              {preview.diff.compatible && (
                <button onClick={() => confirmLoad('preserve')}>保留兼容状态载入</button>
              )}
              <button className="danger" onClick={() => confirmLoad('restart')}>明确重新开始</button>
              <button onClick={() => setPreview(null)}>取消</button>
            </div>
          </div>
        </div>
      )}
    </main>
  );
}
