import type { NodeKind } from "./types";

const palette: { kind: NodeKind; title: string; hint: string }[] = [
  { kind: "contact", title: "常开触点 ┤├", hint: "变量：in.* / m.* / out.* / t.*" },
  { kind: "ncontact", title: "常闭触点 ┤/├", hint: "串联互锁，例如 in.stop" },
  { kind: "or", title: "分支汇合 OR", hint: "并联支路必须经此汇合" },
  { kind: "coil", title: "线圈 ( )", hint: "目标：out.* 或 m.*（保持）" },
  { kind: "ton", title: "通电延时 TON", hint: "名称 t.*，延时毫秒" },
];

let seq = 0;

export function Palette({ onAdd }: { onAdd: (kind: NodeKind) => void }) {
  return (
    <div className="palette">
      <div className="panel-title">元件库</div>
      {palette.map((p) => (
        <button
          key={p.kind}
          className="palette-item"
          draggable
          title={p.hint}
          onClick={() => onAdd(p.kind)}
          onDragStart={(e) => {
            e.dataTransfer.setData("application/x-node-kind", p.kind);
          }}
        >
          <strong>{p.title}</strong>
          <span>{p.hint}</span>
        </button>
      ))}
      <p className="hint">拖入或点击添加；在节点之间拖线即串联。并联支路用 OR 汇合。</p>
    </div>
  );
}

export function newId(kind: string): string {
  seq += 1;
  return `${kind}-${Date.now().toString(36)}-${seq}`;
}
