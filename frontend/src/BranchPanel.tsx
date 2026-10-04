import { useMemo, useState } from 'react';
import { GitBranch, BookmarkPlus, RotateCcw, FlaskConical, Share2, Ban, Search, Download, FolderOpen, Crosshair } from 'lucide-react';
import { api } from './api';
import type { Branch, Comparison, ControllerState, ReviewNote, ReviewPackage, RowOrigin, ScanRecord } from './types';

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

function ComparisonPanel({ comparison, labelOf }: { comparison: Comparison; labelOf: (id: string) => string }) {
  const divergence = comparison.divergence;
  return (
    <div className={`comparison-panel ${comparison.status}`}>
      <p className={comparison.status === 'diverged' ? 'divergence' : 'branch-meta'}>
        {comparison.status === 'diverged' && '已比较：存在真实差异。'}
        {comparison.status === 'consistent' && '已比较：确实一致。'}
        {comparison.status === 'missing' && '没有比较对象。'}
        {comparison.message}
      </p>
      <p className="branch-meta">
        对照依据：程序修订 v{comparison.programVersion} · 本分支输入版本 #{comparison.inputVersion}
        {comparison.parentInputVersion ? ` · 父分支输入版本 #${comparison.parentInputVersion}` : ''}
        {' · '}对照版本 #{comparison.compareVersion} · 扫描范围 #{comparison.fromScan}-#{comparison.toScan}
        {' · '}已比较 {comparison.comparedScans} 个扫描
        {comparison.missingScans.length > 0 ? ` · 缺失对照：${comparison.missingScans.map((s) => `#${s}`).join(', ')}` : ''}
      </p>
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
    </div>
  );
}

function rangesText(report: Comparison): string {
  if (report.missingRanges.length === 0) return '无';
  return report.missingRanges.map((range) => range.from === range.to ? `#${range.from}` : `#${range.from}-#${range.to}`).join(', ');
}

function formatRecord(record: ScanRecord | undefined, labelOf: (id: string) => string): string {
  if (!record) return '缺失对照记录';
  const outputs = Object.entries(record.outputs).filter(([, value]) => value).map(([id]) => labelOf(id)).join('、') || '无输出';
  const timers = Object.entries(record.timers).map(([id, timer]) => `${labelOf(id)} ${timer.elapsedMs}ms/Q=${timer.q ? '1' : '0'}`).join('，') || '无定时器';
  return `输出：${outputs}；定时状态：${timers}`;
}

function ScanLocator({ report, branch, labelOf }: { report: Comparison; branch: Branch; labelOf: (id: string) => string }) {
  const initialScan = report.divergence?.scan ?? report.fromScan;
  const [scan, setScan] = useState(initialScan);
  const candidate = report.candidateRecords?.find((record) => record.scan === scan) ?? branch.records.find((record) => record.scan === scan);
  const baseline = report.baselineRecords?.find((record) => record.scan === scan);
  const isMissing = report.missingScans.includes(scan);
  return (
    <details className="scan-locator">
      <summary><Crosshair size={13} />按扫描序号定位</summary>
      <div className="locator-row">
        <label>
          扫描序号
          <input type="number" value={scan} min={report.fromScan} max={report.toScan}
            onChange={(event) => setScan(Number(event.target.value))} />
        </label>
        {report.divergence && <button onClick={() => setScan(report.divergence!.scan)}>跳到首次差异 #{report.divergence.scan}</button>}
      </div>
      <p className="branch-meta">实际比较扫描：{report.comparedScanNumbers.map((item) => `#${item}`).join(', ') || '无'}；缺失区间：{rangesText(report)}</p>
      <p className="branch-meta">{isMissing && <b>该扫描缺少对照记录，不参与一致判断。</b>}</p>
      <p className="branch-meta">候选分支：{formatRecord(candidate, labelOf)}</p>
      <p className="branch-meta">历史对照：{formatRecord(baseline, labelOf)}</p>
    </details>
  );
}

function NoteForm({ branch, compareVersion, runAction }: { branch: Branch; compareVersion: number; runAction: Props['runAction'] }) {
  const [text, setText] = useState('');
  if (branch.status === 'abandoned') return null;
  return (
    <div className="note-form">
      <textarea placeholder={`记录针对报告版本 #${compareVersion} 的审阅说明`} value={text}
        onChange={(event) => setText(event.target.value)} />
      <button disabled={!text.trim()} onClick={() => runAction(async () => {
        const next = await api.addReviewNote(branch.id, compareVersion, text.trim());
        setText('');
        return next;
      })}>保存说明</button>
    </div>
  );
}

function ReviewNotes({ notes }: { notes: ReviewNote[] }) {
  if (notes.length === 0) return <p className="branch-meta">暂无审阅说明。</p>;
  return <ul className="review-notes">{notes.map((note, index) => (
    <li key={`${note.compareVersion}-${note.createdAt}-${index}`}>
      <b>报告 #{note.compareVersion}</b><time>{note.createdAt}</time><span>{note.text}</span>
    </li>
  ))}</ul>;
}

function BranchCard({ branch, parent, runAction }: { branch: Branch; parent?: Branch; runAction: Props['runAction'] }) {
  const restartable = branch.program.nodes.filter((node) => node.kind === 'TON' || node.kind === 'SR');
  const [nodeId, setNodeId] = useState(restartable[0]?.id ?? '');
  const labelOf = (id: string) => branch.program.nodes.find((node) => node.id === id)?.label ?? id;
  const origins = branch.origins ?? [];
  const records = branch.records ?? [];
  const restarts = branch.restarts ?? [];
  const notes = branch.overlapNotes ?? [];

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
      {branch.comparison && (
        <>
          <ComparisonPanel comparison={branch.comparison} labelOf={labelOf} />
          <ScanLocator report={branch.comparison} branch={branch} labelOf={labelOf} />
          <NoteForm branch={branch} compareVersion={branch.comparison.compareVersion} runAction={runAction} />
        </>
      )}
      {branch.parentId && branch.status !== 'abandoned' && (
        <div className="rebase-row">
          {parent && parent.inputVersion !== (branch.comparison?.parentInputVersion ?? 0) && (
            <p className="branch-meta">
              父分支已更新到输入版本 #{parent.inputVersion}，本分支对照仍冻结在
              #{branch.comparison?.parentInputVersion ?? 0}，可显式建立新的对照版本。
            </p>
          )}
          <button onClick={() => runAction(() => api.rebaseComparison(branch.id))}>
            <GitBranch size={14} />按父分支当前记录建立新的对照版本（旧报告保留）
          </button>
        </div>
      )}
      {(branch.reports ?? []).length > 0 && (
        <details className="origins">
          <summary>历史对照报告（{(branch.reports ?? []).length}）</summary>
          <div className="report-history">
            {(branch.reports ?? []).map((report) => (
              <section key={report.compareVersion} className="historical-report">
                <h4>历史对照版本 #{report.compareVersion}</h4>
                <ComparisonPanel comparison={report} labelOf={labelOf} />
                <ScanLocator report={report} branch={branch} labelOf={labelOf} />
                <NoteForm branch={branch} compareVersion={report.compareVersion} runAction={runAction} />
              </section>
            ))}
          </div>
        </details>
      )}
      <ReviewNotes notes={branch.reviewNotes ?? []} />
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
      <a className="export-review" href={api.reviewPackageURL(branch.id)} download={`review-${branch.id}.json`}>
        <Download size={14} />导出审阅包
      </a>
    </div>
  );
}

function ReviewPackageViewer({ pkg, onClose }: { pkg: ReviewPackage; onClose: () => void }) {
  const branch = pkg.branch;
  const labelOf = (id: string) => branch.program.nodes.find((node) => node.id === id)?.label ?? id;
  return (
    <section className="review-package-viewer">
      <div className="review-package-head">
        <h2><FolderOpen size={16} />已打开审阅包（独立快照）</h2>
        <button onClick={onClose}>关闭审阅包</button>
      </div>
      <p className="branch-meta">
        格式 {pkg.formatVersion} · 导出时间 {pkg.exportedAt} · {branch.name} · 状态 {branch.status}
        {pkg.boundary ? ` · 边界 ${pkg.boundary.label} #${pkg.boundary.scan}/v${pkg.boundary.version}` : ''}
      </p>
      {[...pkg.reports].sort((a, b) => b.compareVersion - a.compareVersion).map((report) => (
        <section key={report.compareVersion} className="historical-report">
          <h3>{report.compareVersion === pkg.currentReport.compareVersion ? '当前报告' : `历史报告 #${report.compareVersion}`} #{report.compareVersion}</h3>
          <ComparisonPanel comparison={report} labelOf={labelOf} />
          <ScanLocator report={report} branch={branch} labelOf={labelOf} />
          <ReviewNotes notes={(pkg.reviewNotes ?? []).filter((note) => note.compareVersion === report.compareVersion)} />
        </section>
      ))}
    </section>
  );
}

export function BranchPanel({ state, runAction }: Props) {
  const boundaries = state.boundaries ?? [];
  const branches = state.branches ?? [];
  const [query, setQuery] = useState({ programVersion: '', inputVersion: '', scan: '' });
  const [found, setFound] = useState<{ branches: Branch[] } | null>(null);
  const [reviewPackage, setReviewPackage] = useState<ReviewPackage | null>(null);
  const [packageError, setPackageError] = useState('');

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
      <div className="branch-toolbar">
        <button onClick={() => runAction(() => api.saveBoundary(''))}>
          <BookmarkPlus size={14} />保存当前扫描边界（#{state.scanNumber}）
        </button>
        <label className="package-open">
          <FolderOpen size={14} />重新打开审阅包
          <input type="file" accept="application/json,.json" onChange={async (event) => {
            const file = event.target.files?.[0];
            if (!file) return;
            try {
              setReviewPackage(await api.openReviewPackage(file));
              setPackageError('');
            } catch (err) {
              setPackageError(err instanceof Error ? err.message : String(err));
            } finally {
              event.target.value = '';
            }
          }} />
        </label>
      </div>
      {packageError && <p className="error">{packageError}</p>}
      {reviewPackage && <ReviewPackageViewer pkg={reviewPackage} onClose={() => setReviewPackage(null)} />}
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
      {shownBranches.map((branch) => (
        <BranchCard key={branch.id} branch={branch}
          parent={branches.find((item) => item.id === branch.parentId)} runAction={runAction} />
      ))}
      {found && shownBranches.length === 0 && <p className="branch-meta">没有匹配该程序版本/输入版本/扫描序号的试验。</p>}
      {found && shownBranches.length > 0 && (
        <p className="branch-meta">共匹配 {matchedIds.size} 个试验分支（含已放弃分支），子分支结果未接回原执行。</p>
      )}
    </section>
  );
}
