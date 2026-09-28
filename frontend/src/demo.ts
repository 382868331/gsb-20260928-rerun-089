import type { Program } from './types';

export const demoProgram: Program = {
  name: '启动停止互锁与延时动作',
  nodes: [
    { id: 'start', kind: 'INPUT', label: '启动 PB1', position: { x: 30, y: 35 } },
    { id: 'stop', kind: 'INPUT', label: '停止 PB2', position: { x: 30, y: 175 } },
    { id: 'runLatch', kind: 'SR', label: '运行保持(停止优先)', position: { x: 270, y: 85 } },
    { id: 'runDelay', kind: 'TON', label: '运行延时 500ms', presetMs: 500, position: { x: 540, y: 85 } },
    { id: 'motor', kind: 'OUTPUT', label: '电机运行灯', position: { x: 810, y: 35 } },
    { id: 'ready', kind: 'OUTPUT', label: '延时就绪灯', position: { x: 810, y: 165 } },
  ],
  edges: [
    { id: 'e-start-set', source: 'start', sourcePort: 'q', target: 'runLatch', targetPort: 'set' },
    { id: 'e-stop-reset', source: 'stop', sourcePort: 'q', target: 'runLatch', targetPort: 'reset' },
    { id: 'e-latch-motor', source: 'runLatch', sourcePort: 'q', target: 'motor', targetPort: 'in' },
    { id: 'e-latch-timer', source: 'runLatch', sourcePort: 'q', target: 'runDelay', targetPort: 'in' },
    { id: 'e-delay-ready', source: 'runDelay', sourcePort: 'q', target: 'ready', targetPort: 'in' },
  ],
};

