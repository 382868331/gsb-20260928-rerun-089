import type { Status } from "./types";

function BitRow({ name, on }: { name: string; on: boolean }) {
  return (
    <div className={`bit-row ${on ? "on" : "off"}`}>
      <span className="bit-name">{name}</span>
      <span className="bit-lamp">{on ? "1" : "0"}</span>
    </div>
  );
}

export function Monitor({
  status,
  onToggleInput,
}: {
  status: Status | null;
  onToggleInput: (name: string, value: boolean) => void;
}) {
  if (!status) return <div className="monitor">加载状态中…</div>;
  const r = status.report;
  const inputs = Object.keys(r.inputs).sort();
  const internals = Object.keys(r.internals).sort();
  const outputs = Object.keys(r.outputs).sort();
  const timers = Object.entries(r.timers).sort(([a], [b]) => a.localeCompare(b));

  return (
    <div className="monitor">
      <div className="panel-title">扫描观察</div>
      <div className="scan-meta">
        <div>程序版本：<b>v{status.programVersion}</b>（{status.programName}）</div>
        <div>扫描周期：{status.tickMs} ms</div>
        <div>已执行扫描：<b>{r.scan}</b></div>
        <div className={status.running ? "run-on" : "run-off"}>
          调度：{status.running ? "运行中" : "已暂停"}
        </div>
      </div>

      <div className="section">输入表（扫描开始时采样）</div>
      {inputs.map((name) => (
        <label className="bit-row input" key={name}>
          <span className="bit-name">{name}</span>
          <input
            type="checkbox"
            checked={!!r.inputs[name]}
            onChange={(e) => onToggleInput(name, e.target.checked)}
          />
          <span className="bit-lamp">{r.inputs[name] ? "1" : "0"}</span>
        </label>
      ))}

      <div className="section">内部保持状态</div>
      {internals.length === 0 && <div className="empty">无</div>}
      {internals.map((name) => (
        <BitRow key={name} name={name} on={!!r.internals[name]} />
      ))}

      <div className="section">定时器（跨扫描保持）</div>
      {timers.length === 0 && <div className="empty">无</div>}
      {timers.map(([name, t]) => (
        <div key={name} className={`bit-row ${t.q ? "on" : "off"}`}>
          <span className="bit-name">{name}</span>
          <span className="timer-bar">
            <i style={{ width: `${Math.min(100, t.elapsedMs / 15)}%` }} />
          </span>
          <span className="bit-lamp">{t.elapsedMs}ms · {t.q ? "Q=1" : "Q=0"}</span>
        </div>
      ))}

      <div className="section">输出表（扫描结束统一提交）</div>
      {outputs.length === 0 && <div className="empty">无</div>}
      {outputs.map((name) => (
        <BitRow key={name} name={name} on={!!r.outputs[name]} />
      ))}
    </div>
  );
}
