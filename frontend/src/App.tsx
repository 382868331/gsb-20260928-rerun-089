import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  Background,
  Controls,
  ReactFlow,
  ReactFlowProvider,
  addEdge,
  applyEdgeChanges,
  applyNodeChanges,
  useEdgesState,
  useNodesState,
  useReactFlow,
  type Connection,
  type Edge,
  type Node,
  type NodeChange,
  type EdgeChange,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import { Play, Pause, Square, StepForward, Upload, RotateCcw, Plus, PencilLine } from 'lucide-react';
import { api } from './api';
import { demoProgram } from './demo';
import { ControlNodeView, nodeTypes, type ControlNodeData } from './ControlNode';
import type { ControllerState, NodeKind, Program } from './types';
import './style.css';

const TARGET_PORTS: Partial<Record<NodeKind, string[]>> = {
  OUTPUT: ['in'],
  AND: ['a', 'b'],
  OR: ['a', 'b'],
  NOT: ['in'],
  SR: ['set', 'reset'],
  TON: ['in'],
};

type FlowNode = Node<ControlNodeData, 'control'>;

function toFlow(program: Program): { nodes: FlowNode[]; edges: Edge[] } {
  return {
    nodes: program.nodes.map((item) => ({
      id: item.id,
      type: 'control',
      position: item.position ?? { x: 80, y: 80 },
      data: { kind: item.kind, label: item.label, presetMs: item.presetMs },
    })),
    edges: program.edges.map((item) => ({
      id: item.id,
      source: item.source,
      sourceHandle: item.sourcePort,
      target: item.target,
      targetHandle: item.targetPort,
    })),
  };
}

function fromFlow(name: string, nodes: FlowNode[], edges: Edge[]): Program {
  return {
    name,
    nodes: nodes.map((node) => ({
      id: node.id,
      kind: node.data.kind,
      label: String(node.data.label),
      position: { x: Math.round(node.position.x), y: Math.round(node.position.y) },
      presetMs: node.data.kind === 'TON' ? Number(node.data.presetMs) || undefined : undefined,
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

function ControllerApp() {
  const [state, setState] = useState<ControllerState | null>(null);
  const [error, setError] = useState('');
  const [programName, setProgramName] = useState('');
  const [nodes, setNodes] = useNodesState<FlowNode>([]);
  const [edges, setEdges] = useEdgesState<Edge>([]);
  const nodeCounter = useRef(0);
  const { screenToFlowPosition } = useReactFlow();

  const refresh = useCallback(async () => {
    try {
      setState(await api.state());
      setError('');
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, []);

  useEffect(() => {
    Promise.all([api.program(), api.state()])
      .then(([program, current]) => {
        const flow = toFlow(program);
        setNodes(flow.nodes);
        setEdges(flow.edges);
        setProgramName(program.name);
        setState(current);
      })
      .catch((err) => setError(err instanceof Error ? err.message : String(err)));
  }, [setNodes, setEdges]);

  useEffect(() => {
    if (state?.mode !== 'running') return;
    const timer = window.setInterval(refresh, 100);
    return () => window.clearInterval(timer);
  }, [state?.mode, refresh]);

  useEffect(() => {
    setNodes((current) => current.map((node) => ({
      ...node,
      data: { ...node.data, active: Boolean(state?.inputs[node.id] ?? state?.internals[node.id] ?? state?.outputs[node.id]) },
    })));
    setEdges((current) => current.map((edge) => ({
      ...edge,
      animated: Boolean(state?.outputs[edge.target]),
    })));
  }, [state, setNodes, setEdges]);

  const runAction = async (action: () => Promise<ControllerState>) => {
    try {
      setState(await action());
      setError('');
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const validateConnection = useCallback((connection: Connection): string | null => {
    const source = nodes.find((node) => node.id === connection.source);
    const target = nodes.find((node) => node.id === connection.target);
    if (!source || !target) return '连接引用的元件不存在';
    if (source.data.kind === 'OUTPUT') return `输出元件 ${source.data.label} 不能作为信号源`;
    if (connection.sourceHandle !== 'q') return `元件 ${source.data.label} 只有 q 输出端口`;
    const allowed = TARGET_PORTS[target.data.kind];
    if (!connection.targetHandle || !allowed?.includes(connection.targetHandle)) {
      return `元件 ${target.data.label} 没有输入端口 ${connection.targetHandle ?? ''}`;
    }
    if (edges.some((edge) => edge.target === target.id && edge.targetHandle === connection.targetHandle)) {
      return `元件 ${target.data.label} 的端口 ${connection.targetHandle} 已有唯一输入连接`;
    }
    return null;
  }, [nodes, edges]);

  const onConnect = useCallback((connection: Connection) => {
    const reason = validateConnection(connection);
    if (reason) {
      setError(reason);
      return;
    }
    setError('');
    setEdges((current) => addEdge({ ...connection, id: `edge-${Date.now()}-${current.length}` }, current));
  }, [setEdges, validateConnection]);

  const onNodesChangeWrapped = useCallback((changes: NodeChange<FlowNode>[]) => {
    setNodes((current) => applyNodeChanges(changes, current));
  }, [setNodes]);

  const onEdgesChangeWrapped = useCallback((changes: EdgeChange<Edge>[]) => {
    setEdges((current) => applyEdgeChanges(changes, current));
  }, [setEdges]);

  const addNode = (kind: NodeKind) => {
    nodeCounter.current += 1;
    const id = `n${Date.now()}-${nodeCounter.current}`;
    const position = screenToFlowPosition({ x: window.innerWidth / 2 + nodeCounter.current * 24, y: window.innerHeight / 2 });
    const data: ControlNodeData = { kind, label: `${kind}-${nodeCounter.current}` };
    if (kind === 'TON') data.presetMs = 1000;
    setNodes((current) => [...current, { id, type: 'control', position, data }]);
  };

  const renameProgram = () => {
    const name = window.prompt('程序名称', programName);
    if (name?.trim()) setProgramName(name.trim());
  };

  const loadEditedProgram = () => runAction(() => api.load(fromFlow(programName, nodes, edges)));

  const resetDemo = async () => {
    try {
      const current = await api.load(demoProgram);
      const flow = toFlow(demoProgram);
      setNodes(flow.nodes);
      setEdges(flow.edges);
      setProgramName(demoProgram.name);
      setState(current);
      setError('');
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const labelOf = useCallback((id: string) => nodes.find((node) => node.id === id)?.data.label ?? id, [nodes]);
  const inputEntries = useMemo(
    () => Object.entries(state?.inputs ?? {}).map(([id, value]) => ({ id, label: labelOf(id), value })),
    [state, labelOf],
  );
  const internalEntries = useMemo(
    () => Object.entries(state?.internals ?? {}).map(([id, value]) => ({ id, label: labelOf(id), value: Boolean(value) })),
    [state, labelOf],
  );

  return (
    <main>
      <header>
        <div>
          <h1>工业控制虚拟控制器 · {programName || state?.programName || ''}</h1>
          <p>显式扫描顺序 · SR 保持 · TON 通电延时 · 元件可增删改 · 仅仿真 I/O</p>
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
        <span className="divider" />
        {(['INPUT', 'OUTPUT', 'AND', 'OR', 'NOT', 'SR', 'TON'] as NodeKind[]).map((kind) => (
          <button key={kind} onClick={() => addNode(kind)}><Plus size={14} />{kind}</button>
        ))}
        <button onClick={renameProgram}><PencilLine size={16} />程序名</button>
        <button className="primary" onClick={loadEditedProgram}><Upload size={16} />保存并发布新版本</button>
        <button onClick={resetDemo}><RotateCcw size={16} />恢复示例</button>
      </section>

      {error && <div className="error">{error}（非法程序不会被发布，当前版本继续运行）</div>}

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
          <h2>采样输入（当前程序）</h2>
          <div className="inputs">
            {inputEntries.length === 0 && <em>程序中还没有 INPUT 元件</em>}
            {inputEntries.map(({ id, label, value }) => (
              <label key={id}>
                <span>{label}</span>
                <button
                  className={value ? 'on' : 'off'}
                  onMouseDown={() => runAction(() => api.setInput(id, true))}
                  onMouseUp={() => runAction(() => api.setInput(id, false))}
                  onMouseLeave={() => state?.inputs[id] && runAction(() => api.setInput(id, false))}
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
          <div className="cards internals">
            {internalEntries.map(({ id, label, value }) => (
              <div key={id} className={`card ${value ? 'on' : 'off'}`}>
                <strong>{label}</strong>
                <span>{value ? '1' : '0'}</span>
              </div>
            ))}
          </div>

          <h2>定时器（同 id 跨版本保留）</h2>
          <div className="timers">
            {Object.entries(state?.timers ?? {}).map(([id, timer]) => (
              <div key={id}>
                <span>{labelOf(id)}</span>
                <progress max={timer.presetMs || 1} value={timer.elapsedMs} />
                <em>{timer.elapsedMs}ms / {timer.presetMs}ms · Q={timer.q ? '1' : '0'}</em>
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

export default function App() {
  return (
    <ReactFlowProvider>
      <ControllerApp />
    </ReactFlowProvider>
  );
}
