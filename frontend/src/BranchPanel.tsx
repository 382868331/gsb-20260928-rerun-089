import { useState } from 'react';
import { GitBranch, BookmarkPlus, RotateCcw, FlaskConical, Trash2, GitFork } from 'lucide-react';
import { api } from './api';
import type { Branch, ControllerState, InputFrame } from './types';

const SEGMENT_ROWS = 3;

type Props = {
  state: ControllerState;
  runAction: (action: () => Promise<ControllerState>) => Promise<boolean>;
};

const segmentOf = (branch: Branch) => branch.segment ?? [];
const recordsOf = (branch: Branch) => branch.records ?? [];
const planOf = (branch: Branch) => branch.inputPlan ?? [];

function frameAt(branch: Branch, row: number): InputFrame | undefined {
  const scan = branch.baseScan + 1 + row;
  return planOf(branch).find((frame) => frame.scan === scan);
}

function SegmentEditor({ branch, runAction }: { branch: Branch; runAction: Props['runAction'] }) {
  const inputNodes = branch.program.nodes.filter((node) => node.kind === 'INPUT');
  const initialGrid = () =>
    Array.from({ length: SEGMENT_ROWS }, (_, row) =>
      inputNodes.map((node) => Boolean(frameAt(branch, row)?.inputs[node.id] ?? branch.baseInputs[node.id])),
    );
  const [fromScan, setFromScan] = useState(branch.baseScan + 1);
  const [grid, setGrid] = useState<boolean[][]>(initialGrid);
  const [baseline, setBaseline] = useState<boolean[][]>(initialGrid);

  const toggle = (row: number, col: number) => {
    setGrid((current) => current.map((line, r) => (r === row ? line.map((value, c) => (c === col ? !value : value)) : line)));
  };

  const submit = () => {
    const inputs = grid.map((line, row) => {
      const overrides: Record<string, boolean> = {};
      line.forEach((value, col) => {
        if (value !== baseline[row]?.[col]) overrides[inputNodes[col].id] = value;
      });
      return overrides;
    });
    void runAction(() => api.replaceSegment(branch.id, fromScan, inputs)).then((ok) => {
      if (!ok) return;
      void api.state().then((state) => {
        const updated = (state.branches ?? []).find((item) => item.id === branch.id);
        if (!updated) return;
        const next = Array.from({ length: SEGMENT_ROWS }, (_, row) =>
          inputNodes.map((node) => {
            const frame = planOf(updated).find((item) => item.scan === fromScan + row);
            return Boolean(frame?.inputs[node.id] ?? updated.baseInputs[node.id]);
          }),
        );
        setBaseline(next);
        setGrid(next);
      });
    });
  };

  return (
    <div className="segment-editor">
      <label>替换起点扫描<input type="number" min={branch.baseScan + 1} value={fromScan} onChange={(event) => setFromScan(Number(event.target.value))} /></label>
      <table><thead><tr><th>扫描</th>{inputNodes.map((node) => <th key={node.id}>{node.label}</th>)}</tr></thead>
        <tbody>{grid.map((line, row) => (<tr key={row}><td>#{fromScan + row}</td>{line.map((value, col) => <td key={col}><input type="checkbox" checked={value} onChange={() => toggle(row, col)} /></td>)}</tr>))}</tbody>
      </table>
      <button onClick={submit} disabled={branch.abandoned}><FlaskConical size={14} />替换输入段并比较</button>
    </div>
  );
}

function StatusBadge({ branch }: { branch: Branch }) {
  if (branch.abandoned) return <span className="branch-status failed">已放弃（记录保留）</span>;
  if (branch.status === 'effective' || recordsOf(branch).length > 0) return <span className="branch-status effective">已经生效</span>;
  return <span className="branch-status pending">尚无结果</span>;
}

function SourceTable({ branch }: { branch: Branch }) {
  const frames = planOf(branch);
  if (frames.length === 0) return null;
  return <details className="source-table"><summary>实际采用输入与来源（{frames.length} 个已执行扫描）</summary><table><tbody>
    {frames.map((frame) => <tr key={frame.scan}><td>#{frame.scan}</td><td>{Object.entries(frame.inputs).map(([id, value]) => `${id}=${value ? 1 : 0}`).join('，')}</td><td>{[...new Set(Object.values(frame.sources))].join('，')}</td></tr>)}
  </tbody></table></details>;
}

function BranchCard({ branch, allBranches, runAction }: { branch: Branch; allBranches: Branch[]; runAction: Props['runAction'] }) {
  const restartable = branch.program.nodes.filter((node) => node.kind === 'TON' || node.kind === 'SR');
  const [nodeId, setNodeId] = useState(restartable[0]?.id ?? '');
  const [childScan, setChildScan] = useState(recordsOf(branch).at(-1)?.scan ?? branch.baseScan + 1);
  const labelOf = (id: string) => branch.program.nodes.find((node) => node.id === id)?.label ?? id;
  const parent = allBranches.find((item) => item.id === branch.parentId);

  return <div className={`branch-card ${branch.abandoned ? 'abandoned' : ''}`}>
    <h3><GitBranch size={14} />{branch.name} <StatusBadge branch={branch} /></h3>
    <p className="branch-meta">{parent ? `父分支 ${parent.name} · ` : ''}起点扫描 #{branch.baseScan} · 程序版本 {branch.baseVersion}{branch.inputVersion > 0 ? ` · 输入版本 ${branch.inputVersion}` : ''} · {branch.programName}{segmentOf(branch).length > 0 ? ` · 已规划 ${segmentOf(branch).length} 个扫描` : ''}</p>
    {branch.lastError && <p className="error">操作失败，原暂停程序、分支和扫描记录均未修改：{branch.lastError}</p>}
    {branch.divergence ? (() => {
      const divergence = branch.divergence!;
      return <><p className="divergence">相对{divergence.baselineName ?? '原执行'}：{divergence.message}</p><p className="branch-meta">差异输出：{divergence.outputs.map(labelOf).join('、') || '无'}；内部布尔：{divergence.internals.map(labelOf).join('、') || '无'}；定时器：{(divergence.timers ?? []).map((id) => `${labelOf(id)}=${divergence.internalStates?.[id]?.elapsedMs ?? 0}ms/Q${divergence.internalStates?.[id]?.q ? 1 : 0}`).join('、') || '无'}</p></>;
    })() : <p className="branch-meta">{recordsOf(branch).length > 0 ? `已执行 ${recordsOf(branch).length} 个扫描，尚无输出差异` : '尚无结果：分支已创建但替换输入尚未生效'}</p>}
    <SourceTable branch={branch} />
    <SegmentEditor branch={branch} runAction={runAction} />
    {recordsOf(branch).length > 0 && <div className="restart-row"><select value={childScan} onChange={(event) => setChildScan(Number(event.target.value))}>{recordsOf(branch).map((record) => <option key={record.scan} value={record.scan}>已执行扫描 #{record.scan}</option>)}</select><button disabled={branch.abandoned} onClick={() => runAction(() => api.createChildBranch(branch.id, childScan, ''))}><GitFork size={14} />从真实记录派生</button></div>}
    {restartable.length > 0 && <div className="restart-row"><select value={nodeId} onChange={(event) => setNodeId(event.target.value)}>{restartable.map((node) => <option key={node.id} value={node.id}>{node.label}</option>)}</select><button disabled={branch.abandoned} onClick={() => nodeId && runAction(() => api.restartElement(branch.id, nodeId))}><RotateCcw size={14} />在起点重置</button></div>}
    {!branch.abandoned && <button className="secondary danger" onClick={() => runAction(() => api.abandonBranch(branch.id))}><Trash2 size={14} />放弃分支但保留记录</button>}
    {branch.restarts.length > 0 && <ul className="restart-log">{branch.restarts.map((event, index) => <li key={index}><b>#{event.scan}</b>{event.message}</li>)}</ul>}
  </div>;
}

export function BranchPanel({ state, runAction }: Props) {
  const boundaries = state.boundaries ?? [];
  const branches = state.branches ?? [];
  return <section className="branches">
    <h2>扫描边界与试验分支</h2>
    <p className="branch-meta">状态：创建请求失败不会产生卡片；“尚无结果”表示分支存在但未执行替换；“已经生效”表示已有真实分支扫描。</p>
    <button onClick={() => runAction(() => api.saveBoundary(''))}><BookmarkPlus size={14} />保存当前扫描边界（#{state.scanNumber}）</button>
    {boundaries.map((boundary) => <div key={boundary.id} className="boundary-row"><span>{boundary.label} · 扫描 #{boundary.scan} · 版本 {boundary.version}</span><button onClick={() => runAction(() => api.createBranch(boundary.id, ''))}>建立试验分支</button></div>)}
    {branches.map((branch) => <BranchCard key={branch.id} branch={branch} allBranches={branches} runAction={runAction} />)}
  </section>;
}
