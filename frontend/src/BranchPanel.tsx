import { useState } from 'react';
import { GitBranch, BookmarkPlus, RotateCcw, FlaskConical } from 'lucide-react';
import { api } from './api';
import type { Branch, ControllerState } from './types';

const SEGMENT_ROWS = 3;

type Props = {
  state: ControllerState;
  runAction: (action: () => Promise<ControllerState>) => void;
};

function SegmentEditor({ branch, runAction }: { branch: Branch; runAction: Props['runAction'] }) {
  const inputNodes = branch.program.nodes.filter((node) => node.kind === 'INPUT');
  const [fromScan, setFromScan] = useState(branch.baseScan + 1);
  const [grid, setGrid] = useState<boolean[][]>(() =>
    Array.from({ length: SEGMENT_ROWS }, (_, row) =>
      inputNodes.map((node) => Boolean(branch.segment[row]?.[node.id])),
    ),
  );

  const toggle = (row: number, col: number) => {
    setGrid((current) => current.map((line, r) => (r === row ? line.map((v, c) => (c === col ? !v : v)) : line)));
  };

  const submit = () => {
    const inputs = grid.map((line) => Object.fromEntries(inputNodes.map((node, col) => [node.id, line[col]])));
    runAction(() => api.replaceSegment(branch.id, fromScan, inputs));
  };

  return (
    <div className="segment-editor">
      <label>
        替换起点扫描
        <input
          type="number"
          min={branch.baseScan + 1}
          value={fromScan}
          onChange={(event) => setFromScan(Number(event.target.value))}
        />
      </label>
      <table>
        <thead>
          <tr>
            <th>扫描</th>
            {inputNodes.map((node) => <th key={node.id}>{node.label}</th>)}
          </tr>
        </thead>
        <tbody>
          {grid.map((line, row) => (
            <tr key={row}>
              <td>#{fromScan + row}</td>
              {line.map((value, col) => (
                <td key={col}>
                  <input type="checkbox" checked={value} onChange={() => toggle(row, col)} />
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
      <button onClick={submit}><FlaskConical size={14} />替换输入段并比较</button>
    </div>
  );
}

function BranchCard({ branch, runAction }: { branch: Branch; runAction: Props['runAction'] }) {
  const restartable = branch.program.nodes.filter((node) => node.kind === 'TON' || node.kind === 'SR');
  const [nodeId, setNodeId] = useState(restartable[0]?.id ?? '');
  const labelOf = (id: string) => branch.program.nodes.find((node) => node.id === id)?.label ?? id;

  return (
    <div className="branch-card">
      <h3><GitBranch size={14} />{branch.name}</h3>
      <p className="branch-meta">
        起点扫描 #{branch.baseScan} · 程序版本 {branch.baseVersion} · {branch.programName}
        {branch.segment.length > 0 ? ` · 已替换 ${branch.segment.length} 个扫描` : ''}
      </p>
      {branch.divergence ? (
        <p className="divergence">{branch.divergence.message}</p>
      ) : (
        <p className="branch-meta">{branch.records.length > 0 ? '与原执行输出一致' : '尚未替换输入段'}</p>
      )}
      {branch.divergence && (
        <p className="branch-meta">
          差异输出：{branch.divergence.outputs.map(labelOf).join('、') || '无'}；
          涉及内部状态：{branch.divergence.internals.map(labelOf).join('、') || '无'}
        </p>
      )}
      <SegmentEditor branch={branch} runAction={runAction} />
      {restartable.length > 0 && (
        <div className="restart-row">
          <select value={nodeId} onChange={(event) => setNodeId(event.target.value)}>
            {restartable.map((node) => <option key={node.id} value={node.id}>{node.label}</option>)}
          </select>
          <button onClick={() => nodeId && runAction(() => api.restartElement(branch.id, nodeId))}>
            <RotateCcw size={14} />在起点重新开始该元件
          </button>
        </div>
      )}
      {branch.restarts.length > 0 && (
        <ul className="restart-log">
          {branch.restarts.map((event, index) => <li key={index}><b>#{event.scan}</b>{event.message}</li>)}
        </ul>
      )}
    </div>
  );
}

export function BranchPanel({ state, runAction }: Props) {
  const boundaries = state.boundaries ?? [];
  const branches = state.branches ?? [];
  return (
    <section className="branches">
      <h2>扫描边界与试验分支</h2>
      <button onClick={() => runAction(() => api.saveBoundary(''))}>
        <BookmarkPlus size={14} />保存当前扫描边界（#{state.scanNumber}）
      </button>
      {boundaries.map((boundary) => (
        <div key={boundary.id} className="boundary-row">
          <span>{boundary.label} · 扫描 #{boundary.scan} · 版本 {boundary.version}</span>
          <button onClick={() => runAction(() => api.createBranch(boundary.id, ''))}>建立试验分支</button>
        </div>
      ))}
      {branches.map((branch) => <BranchCard key={branch.id} branch={branch} runAction={runAction} />)}
    </section>
  );
}

