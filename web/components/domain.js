// Domain 组件 —— 设计系统 §5.2，由 Primitives 组合，不得自定义颜色
import { h } from '../lib/dom.js';
import { Badge } from './primitives.js';

const STATE_MAP = {
  'self': { variant: 'self', icon: '★', text: '本工具' },
  'system': { variant: 'system', icon: '◆', text: '系统' },
  'rproxy': { variant: 'rproxy', icon: '⇄', text: '反代' },
  'in-use(docker)': { variant: 'docker', icon: '▣', text: '容器' },
  'in-use(host)': { variant: 'host', icon: '⚙', text: '进程' },
  'configured': { variant: 'configured', icon: '◇', text: '已声明' },
  'reserved': { variant: 'reserved', icon: '✓', text: '已保留' },
  'free': { variant: 'free', icon: '○', text: '空闲' },
};

export const STATE_COLOR = {
  'system': 'var(--pl-n-100)',
  'rproxy': 'var(--pl-brand-200)',
  'in-use(docker)': 'var(--pl-n-300)',
  'in-use(host)': 'var(--pl-n-400)',
  'configured': 'var(--pl-n-200)',
  'reserved': 'var(--pl-info-600)',
  'self': 'var(--pl-brand-800)',
  'free': 'var(--pl-success-600)',
};

export const STATE_LABEL = Object.fromEntries(Object.entries(STATE_MAP).map(([k, v]) => [k, v.text]));
export const KNOWN_STATES = Object.keys(STATE_MAP);

export function PortBadge(item) {
  const m = STATE_MAP[item.state] || { variant: 'neutral', icon: '', text: item.state };
  const els = [Badge(m)];
  if (item.conflict) els.push(Badge({ variant: 'conflict', icon: '!', text: '冲突' }));
  return h('span', { style: { display: 'inline-flex', gap: '4px', flexWrap: 'wrap' } }, ...els);
}

export function PortNumber(port) {
  return h('span.pl-cell-port', null, String(port));
}

export function SourceLabel(item) {
  const text = item.source || '—';
  return h('span.pl-ellipsis', { title: text, style: { color: 'var(--pl-n-600)' } }, text);
}

export function NameCell(item) {
  const wrap = h('span.pl-ellipsis', { title: [item.name, item.note].filter(Boolean).join(' · ') || '—' });
  if (!item.name) { wrap.textContent = '—'; return wrap; }
  wrap.appendChild(document.createTextNode(item.name));
  if (item.name_source === 'auto') {
    wrap.appendChild(h('span', { style: { color: 'var(--pl-n-400)', fontSize: 'var(--pl-font-xs)', marginLeft: '5px' } }, '自动'));
  }
  return wrap;
}

export function HealthIndicator(health, { compact = false } = {}) {
  const list = health || [];
  const enabled = list.filter(x => x.enabled !== false);
  const ok = enabled.filter(x => x.ok).length;
  const total = enabled.length || 0;
  const bad = total - ok;
  const cls = bad === 0 ? '' : (ok === 0 ? 'pl-dot--danger' : 'pl-dot--warn');
  const label = total === 0 ? '数据源未启用' : `${ok}/${total} 正常`;
  return h('span.pl-health', { title: list.map(x => `${x.name}: ${x.ok ? '正常' : (x.error || '异常')}`).join('\n') },
    h('span.pl-dot', { class: cls }),
    compact ? label : (bad === 0 ? label : `${label} · 部分数据可能不全`),
  );
}

export function RangeBar(counts) {
  const keys = ['system', 'rproxy', 'in-use(docker)', 'in-use(host)', 'configured', 'reserved', 'self', 'free'];
  const total = keys.reduce((s, k) => s + (counts[k] || 0), 0) || 1;
  const bar = h('div.pl-rangebar');
  for (const k of keys) {
    const n = counts[k] || 0;
    if (!n) continue;
    const seg = h('span', { title: `${STATE_LABEL[k]} ${n}`, style: { background: STATE_COLOR[k], flexGrow: String(Math.max(n / total, 0.004)) } });
    bar.appendChild(seg);
  }
  const legend = h('div.pl-legend');
  for (const k of keys) {
    if (!(counts[k] > 0)) continue;
    legend.appendChild(h('span', null, h('i', { style: { background: STATE_COLOR[k] } }), `${STATE_LABEL[k]} ${counts[k]}`));
  }
  return [bar, legend];
}

export function Metric(label, value, { sub, onClick } = {}) {
  const el = h(onClick ? 'button.pl-metric' : 'div.pl-metric', { type: onClick ? 'button' : null, onClick: onClick || null },
    h('div.pl-metric-label', null, label),
    h('div.pl-metric-value', null, String(value)),
    sub ? h('div.pl-metric-sub', null, sub) : null);
  return el;
}
