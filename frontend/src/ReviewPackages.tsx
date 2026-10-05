import { useEffect, useMemo, useState } from 'react';
import { Download, FileArchive, FolderOpen, MessageSquareText } from 'lucide-react';
import { api } from './api';
import type { Comparison, ReviewPackage } from './types';

type Props = {
  runAction: <T>(action: () => Promise<T>) => Promise<T | false>;
};

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
  const status = report.status === 'diverged' ? '存在差异' : report.status === 'consistent' ? '确实一致' : '缺少对照';
  return (
    <div className={`comparison-panel ${report.status}`}>
      <b>对照版本 #{report.compareVersion}</b> · {status} · 输入版本 #{report.inputVersion}
      {report.parentInputVersion ? ` · 父输入版本 #${report.parentInputVersion}` : ''}
      {' · '}#{report.fromScan}-#{report.toScan} · 已比较 {report.comparedScans}
      {report.divergence ? ` · 首次差异 #${report.divergence.scan}` : ''}
      {(report.missingScans ?? []).length > 0 ? ` · 缺失 ${report.missingScans.length}` : ''}
      <p className="branch-meta">{report.message}</p>
    </div>
  );
}

function ReviewPackageCard({ reviewPackage, runAction, refresh }: {
  reviewPackage: ReviewPackage;
  runAction: Props['runAction'];
  refresh: () => Promise<void>;
}) {
  const [compareVersion, setCompareVersion] = useState(reviewPackage.currentReport.compareVersion);
  const [scan, setScan] = useState(reviewPackage.currentReport.divergence?.scan ?? reviewPackage.comparedScans[0]?.scan ?? 0);
  const [text, setText] = useState('');
  const labelOf = (id: string) => reviewPackage.program.nodes.find((node) => node.id === id)?.label ?? id;
  const selectedReport = useMemo(
    () => [reviewPackage.currentReport, ...reviewPackage.historicalReports].find((report) => report.compareVersion === compareVersion),
    [compareVersion, reviewPackage],
  );
  const selectedScan = reviewPackage.comparedScans.find((row) => row.scan === scan);
  const notes = reviewPackage.notes.filter((note) => note.compareVersion === compareVersion && (!note.scan || note.scan === scan || !scan));
  const selectReport = (version: number) => {
    setCompareVersion(version);
    const report = [reviewPackage.currentReport, ...reviewPackage.historicalReports].find((item) => item.compareVersion === version);
    if (report?.divergence) setScan(report.divergence.scan);
  };

  const submitNote = async () => {
    if (!text.trim()) return;
    await runAction(() => api.addReviewNote(reviewPackage.id, compareVersion, scan, text.trim()));
    setText('');
    await refresh();
  };

  return (
    <details className="review-package">
      <summary>
        <FileArchive size={14} />{reviewPackage.branchName} · 当前对照 #{reviewPackage.currentReport.compareVersion}
        {' · '}{reviewPackage.comparedScans.length} 个扫描 · {reviewPackage.notes.length} 条说明
      </summary>
      <p className="branch-meta">
        {reviewPackage.branchStatus === 'abandoned' ? '导出后中间层已放弃，包仍独立可读。' : '自包含快照，不依赖当前父分支。'}
        {' '}程序版本 {reviewPackage.programVersion} · 分支输入版本 #{reviewPackage.inputVersion} · 导出 {new Date(reviewPackage.exportedAt).toLocaleString()}
      </p>
      <ReportSummary report={reviewPackage.currentReport} />
      {reviewPackage.historicalReports.map((report) => <ReportSummary key={report.compareVersion} report={report} />)}
      <p className="branch-meta">缺失区间：{reviewPackage.missingIntervals.map((interval) => `#${interval.fromScan}-#${interval.toScan}`).join('，') || '无'}</p>

      <div className="review-controls">
        <label>报告版本
          <select value={compareVersion} onChange={(event) => selectReport(Number(event.target.value))}>
            {[reviewPackage.currentReport, ...reviewPackage.historicalReports].map((report) => (
              <option key={report.compareVersion} value={report.compareVersion}>#{report.compareVersion}</option>
            ))}
          </select>
        </label>
        <label>扫描序号
          <input type="number" value={scan} onChange={(event) => setScan(Number(event.target.value))} />
        </label>
          <button onClick={() => selectedReport?.divergence && setScan(selectedReport.divergence.scan)} disabled={!selectedReport?.divergence}>定位首次差异</button>
        <button onClick={() => downloadReviewPackage(reviewPackage)}><Download size={14} />再次导出</button>
      </div>

      {selectedReport && <ReportSummary report={selectedReport} />}
      {selectedScan ? (
        <div className="review-scan">
          <b>扫描 #{selectedScan.scan} · {selectedScan.status === 'compared' ? '实际比较' : '缺失基线'}</b>
          {(selectedScan.firstOutputDifference || selectedReport?.divergence?.scan === selectedScan.scan) && (
            <span className="status-badge applied">首次输出差异</span>
          )}
          <p className="branch-meta">
            输出差异：{(selectedScan.outputDifferences ?? []).map(labelOf).join('、') || '无'}；
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
        {notes.map((note) => <li key={note.id}>{note.scan ? `#${note.scan} · ` : ''}{note.text}</li>)}
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
