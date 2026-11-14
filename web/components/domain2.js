// Domain 组件（续）：PortPicker —— 设计系统 §5.2
import { h } from '../lib/dom.js';

export function PortPicker(port, { onReserve, onCopy } = {}) {
  return h('span.pl-tag', { style: { paddingRight: '2px' } },
    h('span', { style: { fontFamily: 'var(--pl-font-mono)' } }, String(port)),
    h('button', { type: 'button', title: '复制 -p 片段', onClick: onCopy }, '⧉'),
    h('button', { type: 'button', title: '保留此端口', onClick: onReserve }, '✓'),
  );
}
