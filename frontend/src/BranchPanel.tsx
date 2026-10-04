import { useMemo, useState } from 'react';
import { GitBranch, BookmarkPlus, RotateCcw, FlaskConical, Share2, Ban, Search, RefreshCw } from 'lucide-react';
import { api } from './api';
import type { Branch, CompareReport, ControllerState, RowOrigin } from './types';

const SEGMENT_ROWS = 3;

type Props = {
  state: ControllerState;
  runAction: (action: () => Promise<ControllerState>) => void;
};

function StatusBadge({ branch }: { branch: Branch }) {
  if (branch.status === 'abandoned') {
    return <span className="status-badge abandoned">已放弃（只读）</span>;
  }
  if (branch.status === 'applied') {
    return <span className="status-badge applied">已生效 · 输入版本 #{branch.inputVersion}</span>;
  }
  return <span className="status-badge created">创建失败重试 / 尚无结果</span>;
}

function originText(origin: RowOrigin): string {
  switch (origin.source) {
    case 'original':
      return '原执行真实扫描';
    case 'parent':
      return `父分支 ${origin.sourceBranchName ?? ''} 模拟（非真实扫描）`;
    case 'self':
      return '本分支替换';
    default:
      return '未知来源';
  }
}

function SegmentEditor({ branch, runAction }: { branch: Branch; runAction: Props['runAction'] }) {
  const inputNodes = branch.program.nodes.filter((node) => node.kind === 'INPUT');
  const segment = branch.segment ?? [];
  const [fromScan, setFromScan] = useState(branch.baseScan + 1);
  const [grid, setGrid] = useState<boolean[][]>(() =>
    Array.from({ length: SEGMENT_ROWS }, (_, row) =>
      inputNodes.map((node) => Boolean(segment[row]?.[node.id])),
    ),
  );

  if (branch.status === 'abandoned') return null;

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
        替换起点扫描（必须晚于分支起点 #{branch.baseScan}{branch.forkScan ? ` / 派生点 #${branch.forkScan}` : ''}）
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

function ForkRow({ branch, runAction }: { branch: Branch; runAction: Props['runAction'] }) {
  const records = branch.records ?? [];
  const [forkScan, setForkScan] = useState(records.length > 0 ? records[records.length - 1].scan : branch.baseScan);
  const [name, setName] = useState('');
  if (branch.status === 'abandoned' || records.length === 0) return null;
  return (
    <div className="fork-row">
      <label>
        派生点扫描
        <input type="number" min={branch.baseScan + 1} max={records[records.length - 1].scan} value={forkScan}
          onChange={(event) => setForkScan(Number(event.target.value))} />
      </label>
      <input className="fork-name" placeholder="子分支名称（可空）" value={name} onChange={(event) => setName(event.target.value)} />
      <button onClick={() => runAction(() => api.forkBranch(branch.id, forkScan, name))}>
        <Share2 size={14} />从此分支派生子分支
      </button>
    </div>
  );
}

// CompareResultView 明确区分三种对照结论：已比较且发现首处真实差异、
// 已比较且确实一致、没有比较对象（缺失记录不当作已执行扫描）。
function CompareResultView({ report, labelOf }: { report: CompareReport; labelOf: (id: string) => string }) {
  if (report.result === 'no-baseline') {
    return <p className="compare-none">{report.message}</p>;
  }
  if (report.result === 'identical') {
    return <p className="branch-meta compare-identical">{report.message}</p>;
  }
  const divergence = report.divergence;
  return (
    <>
      <p className="divergence">{report.message}</p>
      {divergence && (
        <>
          <p className="branch-meta">
            差异输出：{divergence.outputs.map(labelOf).join('、') || '无'}；
            涉及内部状态：{divergence.internals.map(labelOf).join('、') || '无'}
          </p>
          <p className="branch-meta">
            当时内部状态：{Object.entries(divergence.internalState ?? {}).map(([id, value]) => `${labelOf(id)}=${value ? '1' : '0'}`).join('，') || '无'}；
            定时累计：{Object.entries(divergence.timerState ?? {}).map(([id, timer]) => `${labelOf(id)} ${timer.elapsedMs}ms/Q=${timer.q ? '1' : '0'}`).join('，') || '无'}
          </p>
        </>
      )}
    </>
  );
}

function BranchCard({ branch, runAction }: { branch: Branch; runAction: Props['runAction'] }) {
  const restartable = branch.program.nodes.filter((node) => node.kind === 'TON' || node.kind === 'SR');
  const [nodeId, setNodeId] = useState(restartable[0]?.id ?? '');
  const labelOf = (id: string) => branch.program.nodes.find((node) => node.id === id)?.label ?? id;
  const origins = branch.origins ?? [];
  const records = branch.records ?? [];
  const restarts = branch.restarts ?? [];
  const notes = branch.overlapNotes ?? [];
  const reports = branch.reports ?? [];
  const latest = reports.length > 0 ? reports[reports.length - 1] : null;

  return (
    <div className={`branch-card ${branch.status === 'abandoned' ? 'abandoned' : ''}`}>
      <h3><GitBranch size={14} />{branch.name}</h3>
      <p className="branch-meta">
        <StatusBadge branch={branch} />
        {branch.parentId
          ? ` 派生自 ${branch.parentName ?? branch.parentId} @ #${branch.forkScan}`
          : ` 边界 ${branch.boundaryId}`}
        {' · '}起点 #{branch.baseScan} · 程序版本 {branch.baseVersion}
        {origins.length > 0 ? ` · 已模拟 ${origins.length} 个扫描` : ''}
      </p>
      {branch.status === 'created' && origins.length === 0 && (
        <p className="branch-meta">尚无结果：分支已创建，但还没有实际采用任何替换输入。</p>
      )}
      {branch.status === 'created' && origins.length > 0 && (
        <p className="branch-meta">已继承父分支候选输入，可直接比较或继续替换。</p>
      )}
      {branch.compareStale && branch.status !== 'abandoned' && (
        <p className="compare-stale">
          父分支输入版本已更新，当前对照依据仍固定在输入版本 #{branch.baseInputVersion} 的快照。
          <button onClick={() => runAction(() => api.rebaselineBranch(branch.id))}>
            <RefreshCw size={14} />建立新的对照版本（保留旧报告）
          </button>
        </p>
      )}
      {latest ? (
        <CompareResultView report={latest} labelOf={labelOf} />
      ) : (
        branch.status === 'applied' && <p className="branch-meta">已模拟 {records.length} 个扫描，尚无对照报告。</p>
      )}
      {reports.length > 1 && (
        <details className="origins">
          <summary>历次对照报告（{reports.length} 份，旧报告保留）</summary>
          <ul>
            {reports.map((report, index) => (
              <li key={index}>
                对照 #{index + 1}：{report.result === 'diverged' ? `首处差异 @#${report.divergence?.scan}` : report.result === 'identical' ? `已比较 ${report.compared} 段且一致` : '没有比较对象'}
                （程序版本 {report.programVersion}，对照输入版本 #{report.baseInputVersion}，范围 #{report.scanFrom}–#{report.scanTo}）
              </li>
            ))}
          </ul>
        </details>
      )}
      {notes.length > 0 && (
        <ul className="overlap-notes">
          {notes.map((note, index) => <li key={index}>{note}</li>)}
        </ul>
      )}
      {origins.length > 0 && (
        <details className="origins">
          <summary>本次实际采用的输入及来源（{origins.length} 个扫描）</summary>
          <ul>
            {origins.map((origin) => (
              <li key={origin.scan}>
                #{origin.scan} [{originText(origin)}]
                {Object.entries(origin.inputs ?? {}).filter(([, value]) => value).map(([id]) => labelOf(id)).join('、') || '无接通输入'}
                {origin.note ? ` — ${origin.note}` : ''}
              </li>
            ))}
          </ul>
        </details>
      )}
      <SegmentEditor branch={branch} runAction={runAction} />
      <ForkRow branch={branch} runAction={runAction} />
      {restartable.length > 0 && branch.status !== 'abandoned' && (
        <div className="restart-row">
          <select value={nodeId} onChange={(event) => setNodeId(event.target.value)}>
            {restartable.map((node) => <option key={node.id} value={node.id}>{node.label}</option>)}
          </select>
          <button onClick={() => nodeId && runAction(() => api.restartElement(branch.id, nodeId))}>
            <RotateCcw size={14} />在起点重新开始该元件
          </button>
        </div>
      )}
      {restarts.length > 0 && (
        <ul className="restart-log">
          {restarts.map((event, index) => <li key={index}><b>#{event.scan}</b>{event.message}</li>)}
        </ul>
      )}
      {branch.status !== 'abandoned' && (
        <button className="abandon-btn" onClick={() => runAction(() => api.abandonBranch(branch.id))}>
          <Ban size={14} />放弃此中间分支（记录保留）
        </button>
      )}
    </div>
  );
}

export function BranchPanel({ state, runAction }: Props) {
  const boundaries = state.boundaries ?? [];
  const branches = state.branches ?? [];
  const [query, setQuery] = useState({ programVersion: '', inputVersion: '', scan: '' });
  const [found, setFound] = useState<{ branches: Branch[] } | null>(null);

  const search = async () => {
    try {
      const result = await api.findTrials({
        programVersion: query.programVersion ? Number(query.programVersion) : 0,
        inputVersion: query.inputVersion ? Number(query.inputVersion) : 0,
        scan: query.scan ? Number(query.scan) : 0,
      });
      setFound({ branches: result.branches ?? [] });
    } catch (err) {
      setFound(null);
      // eslint-disable-next-line no-alert
      alert(err instanceof Error ? err.message : String(err));
    }
  };

  const shownBranches = found ? found.branches : branches;
  const matchedIds = useMemo(() => new Set(found ? found.branches.map((branch) => branch.id) : []), [found]);

  return (
    <section className="branches">
      <h2>扫描边界与试验分支</h2>
      <button onClick={() => runAction(() => api.saveBoundary(''))}>
        <BookmarkPlus size={14} />保存当前扫描边界（#{state.scanNumber}）
      </button>
      <div className="trial-search">
        <Search size={14} />
        <input placeholder="程序版本" value={query.programVersion}
          onChange={(event) => setQuery((current) => ({ ...current, programVersion: event.target.value }))} />
        <input placeholder="输入版本" value={query.inputVersion}
          onChange={(event) => setQuery((current) => ({ ...current, inputVersion: event.target.value }))} />
        <input placeholder="扫描序号" value={query.scan}
          onChange={(event) => setQuery((current) => ({ ...current, scan: event.target.value }))} />
        <button onClick={search}>找回试验</button>
        {found && <button onClick={() => setFound(null)}>清除筛选</button>}
      </div>
      {!found && boundaries.map((boundary) => (
        <div key={boundary.id} className="boundary-row">
          <span>{boundary.label} · 扫描 #{boundary.scan} · 版本 {boundary.version}</span>
          <button onClick={() => runAction(() => api.createBranch(boundary.id, ''))}>建立试验分支</button>
        </div>
      ))}
      {shownBranches.map((branch) => <BranchCard key={branch.id} branch={branch} runAction={runAction} />)}
      {found && shownBranches.length === 0 && <p className="branch-meta">没有匹配该程序版本/输入版本/扫描序号的试验。</p>}
      {found && shownBranches.length > 0 && (
        <p className="branch-meta">共匹配 {matchedIds.size} 个试验分支（含已放弃分支），子分支结果未接回原执行。</p>
      )}
    </section>
  );
}
