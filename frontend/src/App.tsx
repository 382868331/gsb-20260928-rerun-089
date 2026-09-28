import { useCallback, useEffect, useMemo, useState } from "react";
import {
  Background,
  Controls,
  ReactFlow,
  addEdge,
  useEdgesState,
  useNodesState,
  type Connection,
  type Edge,
  type Node,
  type NodeChange,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { api } from "./api";
import { nodeTypes } from "./LadderNodeView";
import { Monitor } from "./Monitor";
import { Palette, newId } from "./Palette";
import type {
  LadderEdge,
  LadderNode,
  NodeKind,
  Program,
  Status,
  ValidateError,
} from "./types";

function toFlowNodes(p: Program, status: Status | null): Node[] {
  return p.nodes.map((n) => ({
    id: n.id,
    type: "ladder",
    position: { x: n.x ?? 0, y: n.y ?? 0 },
    data: { ladder: n, report: status?.report },
  }));
}

function toFlowEdges(p: Program): Edge[] {
  return p.edges.map((e) => ({ id: e.id, source: e.source, target: e.target }));
}

function fromFlow(
  name: string,
  version: number,
  nodes: Node[],
  edges: Edge[],
): Program {
  return {
    name,
    version,
    nodes: nodes.map((n) => n.data.ladder as LadderNode).map((l, i) => {
      const pos = nodes[i].position;
      return { ...l, x: pos.x, y: pos.y };
    }),
    edges: edges.map((e): LadderEdge => ({ id: e.id, source: e.source, target: e.target })),
  };
}

const defaults: Record<string, Partial<LadderNode>> = {
  contact: { variable: "in.x" },
  ncontact: { variable: "in.x" },
  coil: { variable: "out.x" },
  ton: { variable: "t.x", delayMs: 1000 },
  or: {},
};

export default function App() {
  const [program, setProgram] = useState<Program | null>(null);
  const [status, setStatus] = useState<Status | null>(null);
  const [nodes, setNodes, onNodesChangeRaw] = useNodesState<Node>([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>([]);
  const [errors, setErrors] = useState<ValidateError[]>([]);
  const [dirty, setDirty] = useState(false);
  const [selectedId, setSelectedId] = useState<string | null>(null);

  const selected = useMemo(
    () => nodes.find((n) => n.id === selectedId) ?? null,
    [nodes, selectedId],
  );

  const loadProgram = useCallback((p: Program, st?: Status | null) => {
    setProgram(p);
    setNodes(toFlowNodes(p, st ?? null));
    setEdges(toFlowEdges(p));
    setDirty(false);
    setErrors([]);
  }, [setNodes, setEdges]);

  useEffect(() => {
    let alive = true;
    Promise.all([api.program(), api.status()]).then(([p, st]) => {
      if (!alive) return;
      setStatus(st);
      loadProgram(p, st);
    }).catch(() => {});
    return () => { alive = false; };
  }, [loadProgram]);

  // Long-poll status; energized rendering reflects the real scan report.
  useEffect(() => {
    let alive = true;
    const tick = async () => {
      while (alive) {
        try {
          const st = await api.status();
          if (!alive) return;
          setStatus(st);
          setNodes((ns) =>
            ns.map((n) => ({
              ...n,
              data: { ...n.data, report: st.report },
            })),
          );
        } catch {
          /* retry */
        }
        await new Promise((res) => setTimeout(res, status?.running ? 200 : 500));
      }
    };
    void tick();
    return () => { alive = false; };
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [status?.running]);

  const onNodesChange = useCallback((changes: NodeChange<Node>[]) => {
    if (changes.some((c) => c.type === "position" && c.dragging) ||
        changes.some((c) => c.type === "remove")) {
      setDirty(true);
    }
    onNodesChangeRaw(changes);
  }, [onNodesChangeRaw]);

  const onConnect = useCallback((conn: Connection) => {
    setDirty(true);
    setEdges((es) => addEdge({ ...conn, id: `wire-${Date.now().toString(36)}` }, es));
  }, [setEdges]);

  const addNode = useCallback((kind: NodeKind, at?: { x: number; y: number }) => {
    const id = newId(kind);
    const ladder: LadderNode = {
      id,
      kind,
      variable: defaults[kind].variable,
      delayMs: defaults[kind].delayMs,
      x: at?.x ?? 250 + Math.random() * 120,
      y: at?.y ?? 120 + Math.random() * 160,
    };
    setNodes((ns) => [...ns, {
      id,
      type: "ladder",
      position: { x: ladder.x!, y: ladder.y! },
      data: { ladder, report: status?.report },
      selected: true,
    }]);
    setSelectedId(id);
    setDirty(true);
  }, [setNodes, status]);

  const updateSelected = (patch: Partial<LadderNode>) => {
    if (!selected) return;
    setNodes((ns) => ns.map((n) => n.id === selected.id
      ? { ...n, data: { ...n.data, ladder: { ...(n.data.ladder as LadderNode), ...patch } } }
      : n));
    setDirty(true);
  };

  const buildProgram = (): Program | null => {
    if (!program) return null;
    return fromFlow(program.name, program.version, nodes, edges);
  };

  const validate = async () => {
    const p = buildProgram();
    if (!p) return;
    const res = await api.validate(p);
    setErrors(res.errors ?? []);
    return res;
  };

  const deploy = async () => {
    const p = buildProgram();
    if (!p) return;
    const next = { ...p, version: program!.version + 1 };
    const res = await api.load(next);
    if (!res.loaded) {
      setErrors(res.errors ?? []);
      return;
    }
    setErrors([]);
    loadProgram(res.program!, status);
    setStatus(await api.status());
  };

  const resetDemo = async () => {
    const res = await api.demo();
    if (res.program) {
      loadProgram(res.program, status);
      setStatus(await api.status());
    }
  };

  const toggleInput = async (name: string, value: boolean) => {
    const st = await api.setInput(name, value);
    setStatus(st);
  };

  const control = async (fn: () => Promise<Status>) => {
    setStatus(await fn());
  };

  return (
    <div className="app">
      <header>
        <h1>工业控制编程平台 · 虚拟控制器</h1>
        <div className="toolbar">
          <button onClick={() => control(api.run)} disabled={status?.running}>▶ 运行（连续扫描）</button>
          <button onClick={() => control(api.pause)} disabled={!status?.running}>⏸ 暂停（保持计时/状态）</button>
          <button onClick={() => control(api.step)}>⏭ 单扫描</button>
          <span className="sep" />
          <button onClick={validate}>校验连接</button>
          <button className="primary" onClick={deploy} disabled={!dirty}>作为新版本载入</button>
          <button onClick={resetDemo}>恢复演示程序</button>
          {dirty && <span className="dirty">● 有未载入的编辑</span>}
        </div>
      </header>
      <div className="body">
        <Palette onAdd={(k) => addNode(k)} />
        <main
          className="canvas-wrap"
          onDragOver={(e) => e.preventDefault()}
          onDrop={(e) => {
            e.preventDefault();
            const kind = e.dataTransfer.getData("application/x-node-kind") as NodeKind;
            if (kind) addNode(kind, { x: e.clientX - 120, y: e.clientY - 80 });
          }}
        >
          <ReactFlow
            nodes={nodes}
            edges={edges}
            nodeTypes={nodeTypes}
            onNodesChange={onNodesChange}
            onEdgesChange={(c) => { setDirty(true); onEdgesChange(c); }}
            onConnect={onConnect}
            onNodeClick={(_, n) => setSelectedId(n.id)}
            onPaneClick={() => setSelectedId(null)}
            deleteKeyCode={["Backspace", "Delete"]}
            fitView
          >
            <Background />
            <Controls />
          </ReactFlow>
          {selected && (
            <div className="inspector">
              <div className="panel-title">节点属性</div>
              <div>类型：{(selected.data.ladder as LadderNode).kind}</div>
              {(selected.data.ladder as LadderNode).variable !== undefined && (
                <label>变量名
                  <input
                    value={(selected.data.ladder as LadderNode).variable ?? ""}
                    onChange={(e) => updateSelected({ variable: e.target.value })}
                  />
                </label>
              )}
              {(selected.data.ladder as LadderNode).kind === "ton" && (
                <label>延时（毫秒）
                  <input
                    type="number"
                    value={(selected.data.ladder as LadderNode).delayMs ?? 0}
                    onChange={(e) => updateSelected({ delayMs: Number(e.target.value) })}
                  />
                </label>
              )}
            </div>
          )}
          {errors.length > 0 && (
            <div className="errors">
              <b>非法连接，程序不会运行：</b>
              <ul>
                {errors.map((er, i) => (
                  <li key={i}>{er.message}{er.nodeId ? `（节点 ${er.nodeId}）` : ""}{er.edgeId ? `（连线 ${er.edgeId}）` : ""}</li>
                ))}
              </ul>
            </div>
          )}
        </main>
        <Monitor status={status} onToggleInput={toggleInput} />
      </div>
      <footer>
        扫描顺序：读输入表 → 拓扑求能流（触点读上一扫描值）→ 定时器更新 → 统一提交内部/输出表。本系统不连接任何真实工业设备。
      </footer>
    </div>
  );
}
