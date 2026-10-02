import { useEffect, useState } from 'react';
import { api } from './api';
import type { BranchInfo, ControllerState, Divergence, InputOverride } from './types';

type PanelProps = {
  state: ControllerState;
  onState: (next: ControllerState) => void;
  onError: (message: string) => void;
};

export function BranchPanel({ state, onState, onError }: PanelProps) {
  const [boundaryName, setBoundaryName] = useState('');

  const run = async (action: () => Promise<ControllerState>) => {
    try {
      onState(await action());
      onError('');
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <>
      <h2>扫描边界</h2>
      <div className="boundary-save">
        <input
          value={boundaryName}
          placeholder={`边界名称（当前扫描 #${state.scanNumber}）`}
          onChange={(event) => setBoundaryName(event.target.value)}
        />
        <button onClick={() => run(() => api.saveBoundary(boundaryName))}>保存边界</button>
      </div>
      <ul className="boundary-list">
        {state.boundaries.map((boundary) => (
          <li key={boundary.id}>
            <span>
              {boundary.name} · 扫描 #{boundary.scan} · 版本 {boundary.version}
            </span>
            <button onClick={() => run(() => api.createBranch(boundary.id, ''))}>建立试验分支</button>
          </li>
        ))}
        {state.boundaries.length === 0 && <li className="hint">暂无已保存边界</li>}
      </ul>

      <h2>输入试验分支</h2>
      {state.branches.length === 0 && <p className="hint">从已保存的扫描边界建立分支，替换后续一段虚拟输入并比较差异。</p>}
      {state.branches.map((branch) => (
        <BranchCard key={branch.id} branch={branch} run={run} onError={onError} />
      ))}
    </>
  );
}

function BranchCard({ branch, run, onError }: {
  branch: BranchInfo;
  run: (action: () => Promise<ControllerState>) => Promise<void>;
  onError: (message: string) => void;
}) {
  const [rows, setRows] = useState<InputOverride[]>(branch.overrides);
  const [result, setResult] = useState<Divergence | null>(null);
  const [resetNode, setResetNode] = useState('');

  useEffect(() => {
    setRows(branch.overrides);
  }, [branch.overrides]);

  const inputNodes = branch.program.nodes.filter((node) => node.kind === 'INPUT');
  const resettable = branch.program.nodes.filter((node) => node.kind === 'TON' || node.kind === 'SR');
  const labelOf = (id: string) => branch.program.nodes.find((node) => node.id === id)?.label ?? id;

  const updateRow = (index: number, patch: Partial<InputOverride>) => {
    setRows((current) => current.map((row, i) => (i === index ? { ...row, ...patch } : row)));
  };

  const addRow = () => {
    setRows((current) => [...current, { scan: branch.scan + 1, input: inputNodes[0]?.id ?? '', value: true }]);
  };

  const compare = async () => {
    try {
      setResult(await api.compareBranch(branch.id));
      onError('');
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <div className="branch">
      <div className="branch-head">
        <strong>{branch.name}</strong>
        <span>起点 #{branch.originScan} · 程序版本 {branch.version} · 当前扫描 #{branch.scan}</span>
      </div>

      <div className="cards">
        {Object.entries(branch.outputs).map(([id, value]) => (
          <div key={id} className={`card ${value ? 'on' : 'off'}`}>
            <strong>{labelOf(id)}</strong>
            <span>{value ? 'ON' : 'OFF'}</span>
          </div>
        ))}
      </div>

      <div className="timers">
        {Object.entries(branch.timers).map(([id, timer]) => (
          <div key={id}>
            <span>{labelOf(id)}</span>
            <em>{timer.elapsedMs}ms · Q={timer.q ? '1' : '0'}（跨起点保留累计）</em>
          </div>
        ))}
      </div>

      <h3>输入覆盖（共同前缀只读，仅起点之后）</h3>
      {rows.map((row, index) => (
        <div className="override-row" key={index}>
          <input
            type="number"
            min={branch.scan + 1}
            value={row.scan}
            onChange={(event) => updateRow(index, { scan: Number(event.target.value) })}
          />
          <select value={row.input} onChange={(event) => updateRow(index, { input: event.target.value })}>
            {inputNodes.map((node) => <option key={node.id} value={node.id}>{node.label}</option>)}
          </select>
          <select value={row.value ? '1' : '0'} onChange={(event) => updateRow(index, { value: event.target.value === '1' })}>
            <option value="1">接通</option>
            <option value="0">断开</option>
          </select>
          <button onClick={() => setRows((current) => current.filter((_, i) => i !== index))}>删除</button>
        </div>
      ))}
      <div className="branch-actions">
        <button onClick={addRow} disabled={inputNodes.length === 0}>添加覆盖</button>
        <button onClick={() => run(() => api.setOverrides(branch.id, rows))}>保存覆盖</button>
        <button onClick={() => run(() => api.scanBranch(branch.id, 1))}>扫描 1 次</button>
        <button onClick={() => run(() => api.scanBranch(branch.id, 5))}>扫描 5 次</button>
        <button onClick={compare}>比较差异</button>
      </div>

      {result && (
        <div className={`divergence ${result.found ? 'found' : ''}`}>
          {result.found ? (
            <>
              <strong>首次输出差异：扫描 #{result.scan}</strong>
              <ul>{result.outputs.map((item) => <li key={item}>{item}</li>)}</ul>
              {result.internals.length > 0 && (
                <>
                  <span>涉及的内部状态：</span>
                  <ul>{result.internals.map((item) => <li key={item}>{item}</li>)}</ul>
                </>
              )}
            </>
          ) : (
            <span>已比较 {result.comparedScans} 个扫描，未发现输出差异。</span>
          )}
        </div>
      )}

      <div className="branch-actions">
        <select value={resetNode} onChange={(event) => setResetNode(event.target.value)}>
          <option value="">选择保持类元件…</option>
          {resettable.map((node) => <option key={node.id} value={node.id}>{node.label}</option>)}
        </select>
        <button
          disabled={!resetNode}
          onClick={() => run(() => api.resetBranchElement(branch.id, resetNode))}
        >重新开始该元件</button>
      </div>

      {branch.resetLog.length > 0 && (
        <ol className="log">
          {branch.resetLog.slice().reverse().map((event, index) => (
            <li key={index}><b>#{event.scan}</b>{event.message}</li>
          ))}
        </ol>
      )}
    </div>
  );
}

