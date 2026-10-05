import { useEffect, useMemo, useState } from 'react';
import { Download, FileArchive, FolderOpen, MessageSquareText } from 'lucide-react';
import { api } from './api';
import type { Comparison, ReviewPackage } from './types';

type Props = {
  runAction: <T>(action: () => Promise<T>) => Promise<T | false>;
};

function asArray<T>(value: T[] | null | undefined): T[] {
  return value ?? [];
}

function normalizePackage(reviewPackage: ReviewPackage): ReviewPackage {
  const program = reviewPackage.program ?? {
    name: '',
    nodes: [],
    edges: [],
  };
  return {
    ...reviewPackage,
    program: {
      ...program,
      nodes: asArray(program.nodes),
      edges: asArray(program.edges),
    },
    currentReport: {
      ...reviewPackage.currentReport,
      missingScans: asArray(reviewPackage.currentReport?.missingScans),
      divergence: reviewPackage.currentReport?.divergence ?? null,
    },
    historicalReports: asArray(reviewPackage.historicalReports).map((report) => ({
      ...report,
      missingScans: asArray(report.missingScans),
      divergence: report.divergence ?? null,
    })),
    comparedScans: asArray(reviewPackage.comparedScans).map((row) => ({
      ...row,
      status: 'compared' as const,
      outputDifferences: asArray(row.outputDifferences),
      internalDifferences: asArray(row.internalDifferences),
      timerState: row.timerState ?? {},
    })),
    missingScans: asArray(reviewPackage.missingScans).map((row) => ({
      ...row,
      status: 'missing' as const,
      baseline: null,
      outputDifferences: [],
      internalDifferences: [],
      firstOutputDifference: false,
      timerState: row.timerState ?? {},
    })),
    missingIntervals: asArray(reviewPackage.missingIntervals),
    bindingDescription: reviewPackage.bindingDescription || '审阅说明绑定到本包内的对照报告版本；缺失扫描不补写基线。',
    notes: asArray(reviewPackage.notes),
  };
}

export function downloadReviewPackage(pkg: ReviewPackage) {
  const blob = new Blob([JSON.stringify(pkg, null, 2)], { type: 'application/json' });
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = `review-${pkg.branchId}-v${pkg.currentReport.compareVersion}.json`;
  link.click();
  URL.revokeObjectURL(url);
}

function ReportSummary({ report }: { report: Comparison }) {
  const safeReport = { ...report, missingScans: asArray(report.missingScans) };
  const status = safeReport.status === 'diverged' ? '存在差异' : safeReport.status === 'consistent' ? '确实一致' : '缺少对照';
  return (
    <div className={`comparison-panel ${safeReport.status}`}>
      <b>对照版本 #{safeReport.compareVersion}</b> · {status} · 输入版本 #{safeReport.inputVersion}
      {safeReport.parentInputVersion ? ` · 父输入版本 #${safeReport.parentInputVersion}` : ''}
      {' · '}#{safeReport.fromScan}-#{safeReport.toScan} · 已比较 {safeReport.comparedScans}
      {safeReport.divergence ? ` · 首次差异 #${safeReport.divergence.scan}` : ''}
      {safeReport.missingScans.length > 0 ? ` · 缺失 ${safeReport.missingScans.length}` : ''}
      <p className="branch-meta">{safeReport.message}</p>
    </div>
  );
}

function ReviewPackageCard({ reviewPackage, runAction, refresh }: {
  reviewPackage: ReviewPackage;
  runAction: Props['runAction'];
  refresh: () => Promise<void>;
}) {
  const pkg = normalizePackage(reviewPackage);
  const [compareVersion, setCompareVersion] = useState(pkg.currentReport.compareVersion);
  const [scan, setScan] = useState(pkg.currentReport.divergence?.scan ?? pkg.comparedScans[0]?.scan ?? pkg.missingScans[0]?.scan ?? 0);
  const [text, setText] = useState('');
  const labelOf = (id: string) => pkg.program.nodes.find((node) => node.id === id)?.label ?? id;
  const selectedReport = useMemo(
    () => [pkg.currentReport, ...pkg.historicalReports].find((report) => report.compareVersion === compareVersion),
    [compareVersion, pkg],
  );
  const allScans = [...pkg.comparedScans, ...pkg.missingScans];
  const selectedScan = allScans.find((row) => row.scan === scan);
  const notes = pkg.notes.filter((note) => note.compareVersion === compareVersion && (!note.scan || note.scan === scan || !scan));
  const selectReport = (version: number) => {
    setCompareVersion(version);
    const report = [pkg.currentReport, ...pkg.historicalReports].find((item) => item.compareVersion === version);
    if (report?.divergence) setScan(report.divergence.scan);
  };

  const submitNote = async () => {
    if (!text.trim()) return;
    await runAction(() => api.addReviewNote(pkg.id, compareVersion, scan, text.trim()));
    setText('');
    await refresh();
  };

  return (
    <details className="review-package">
      <summary>
        <FileArchive size={14} />{pkg.branchName} · 当前对照 #{pkg.currentReport.compareVersion}
        {' · '}{pkg.comparedScans.length} 个实际比较 · {pkg.missingScans.length} 个缺失 · {pkg.notes.length} 条说明
      </summary>
      <p className="branch-meta">
        {pkg.branchStatus === 'abandoned' ? '导出后中间层已放弃，包仍独立可读。' : '自包含快照，不依赖当前父分支。'}
        {' '}程序版本 {pkg.programVersion} · 分支输入版本 #{pkg.inputVersion} · 导出 {new Date(pkg.exportedAt).toLocaleString()}
      </p>
      <p className="branch-meta">{pkg.bindingDescription}</p>
      <ReportSummary report={pkg.currentReport} />
      {pkg.historicalReports.map((report) => <ReportSummary key={report.compareVersion} report={report} />)}
      <p className="branch-meta">实际比较扫描：{pkg.comparedScans.map((row) => `#${row.scan}`).join('，') || '无'}</p>
      <p className="branch-meta">缺失扫描：{pkg.missingScans.map((row) => `#${row.scan}`).join('，') || '无'}</p>
      <p className="branch-meta">缺失区间：{pkg.missingIntervals.map((interval) => `#${interval.fromScan}-#${interval.toScan}`).join('，') || '无'}</p>

      <div className="review-controls">
        <label>报告版本
          <select value={compareVersion} onChange={(event) => selectReport(Number(event.target.value))}>
            {[pkg.currentReport, ...pkg.historicalReports].map((report) => (
              <option key={report.compareVersion} value={report.compareVersion}>#{report.compareVersion}</option>
            ))}
          </select>
        </label>
        <label>扫描序号
          <input type="number" value={scan} onChange={(event) => setScan(Number(event.target.value))} />
        </label>
          <button onClick={() => selectedReport?.divergence && setScan(selectedReport.divergence.scan)} disabled={!selectedReport?.divergence}>定位首次差异</button>
        <button onClick={() => downloadReviewPackage(pkg)}><Download size={14} />再次导出</button>
      </div>

      {selectedReport && <ReportSummary report={selectedReport} />}
      {selectedScan ? (
        <div className="review-scan">
          <b>扫描 #{selectedScan.scan} · {selectedScan.status === 'compared' ? '实际比较' : '缺失基线'}</b>
          {(selectedScan.firstOutputDifference || selectedReport?.divergence?.scan === selectedScan.scan) && (
            <span className="status-badge applied">首次输出差异</span>
          )}
          <p className="branch-meta">
            {selectedScan.status === 'compared' ? `输出差异：${selectedScan.outputDifferences.map(labelOf).join('、') || '无'}；` : '该扫描没有发生对照，不补写基线；'}
            定时状态：{Object.entries(selectedScan.timerState ?? {}).map(([id, timer]) => `${labelOf(id)} ${timer.elapsedMs}ms/Q=${timer.q ? 1 : 0}`).join('，') || '无'}
          </p>
        </div>
      ) : (
        <p className="branch-meta">该扫描不在审阅包范围内。</p>
      )}

      <textarea value={text} onChange={(event) => setText(event.target.value)} placeholder="记录针对该报告版本/扫描序号的审阅说明" />
      <button onClick={submitNote} disabled={!text.trim()}>
        <MessageSquareText size={14} />保存说明
      </button>
      <ul className="review-notes">
        {notes.map((note) => <li key={note.id}>报告 #{note.compareVersion}{note.scan ? ` / #${note.scan}` : ' / 整份报告'} · {note.text}</li>)}
      </ul>
    </details>
  );
}

export function ReviewPanel({ runAction }: Props) {
  const [packages, setPackages] = useState<ReviewPackage[]>([]);
  const [error, setError] = useState('');

  const refresh = async () => {
    try {
      setPackages((await api.reviewPackages()).reviews);
      setError('');
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  useEffect(() => { refresh(); }, []);
  useEffect(() => {
    window.addEventListener('review-packages-changed', refresh);
    return () => window.removeEventListener('review-packages-changed', refresh);
  }, []);

  const importFile = async (file: File) => {
    try {
      const imported = JSON.parse(await file.text()) as ReviewPackage;
      await runAction(() => api.importReviewPackage(imported));
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <section className="review-packages">
      <h2>审阅包</h2>
      {error && <p className="modal-warning">{error}</p>}
      <label className="import-review">
        <FolderOpen size={14} />重新打开审阅包
        <input type="file" accept="application/json,.json" onChange={(event) => event.target.files?.[0] && importFile(event.target.files[0])} />
      </label>
      {packages.length === 0 && <p className="branch-meta">尚无审阅包；先在分支报告中导出。</p>}
      {packages.map((pkg) => <ReviewPackageCard key={pkg.id} reviewPackage={pkg} runAction={runAction} refresh={refresh} />)}
    </section>
  );
}
