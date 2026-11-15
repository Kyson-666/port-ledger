// P4 空闲端口 —— 模式 P-C（查询 + 结果 + 复制/占位）
import { h, fmtTime } from '../lib/dom.js';
import { api } from '../lib/api.js';
import { Button, Card, Field, Input, Select, Switch, EmptyState, toast, Badge } from '../components/primitives.js';
import { PortPicker } from '../components/domain2.js';

let st = { from: 20000, to: 30000, count: 5, protocol: 'tcp', contiguous: false, result: null, error: '', loading: false };

export async function render(ctx) {
  const wrap = h('div');
  wrap.appendChild(h('div.pl-page-head', null, h('h1', null, '空闲端口')));

  const fromI = Input({ type: 'number', value: st.from, min: 1, max: 65535 });
  const toI = Input({ type: 'number', value: st.to, min: 1, max: 65535 });
  const cntI = Input({ type: 'number', value: st.count, min: 1, max: 50 });
  const protoS = Select([{ value: 'tcp', label: 'TCP' }, { value: 'udp', label: 'UDP' }], { value: st.protocol });
  // Switch 自带 label 参数：开关与文字同处一个 <label>，点击文字也能切换，间距由组件保证
  const contS = Switch({ checked: st.contiguous, label: '要求连续端口' });

  const form = Card([
    h('div.pl-card-title', null, '查询条件'),
    h('div.pl-grid2', null,
      Field('起始端口', fromI),
      Field('结束端口', toI),
      Field('需要数量', cntI),
      Field('协议', protoS),
    ),
    h('div.pl-field', null, contS),
    h('div', { style: { display: 'flex', gap: '8px' } },
      Button('查询', { variant: 'primary', onClick: () => doQuery(ctx) }),
      Button('重新扫描', { variant: 'text', onClick: async () => { await ctx.scan(); toast('扫描完成'); ctx.rerender(); } }),
    ),
  ]);
  wrap.appendChild(form);

  const resultBox = h('div');
  wrap.appendChild(resultBox);

  function doQueryPage() {
    st.from = Number(fromI.value) || 0;
    st.to = Number(toI.value) || 0;
    st.count = Number(cntI.value) || 1;
    st.protocol = protoS.value;
    st.contiguous = contS.querySelector('input').checked;
  }
  async function doQuery(c) {
    doQueryPage();
    if (st.from < 1 || st.to > 65535 || st.from > st.to) { toast('端口范围不合法', 'danger'); return; }
    if (st.count < 1 || st.count > 50) { toast('数量需在 1-50', 'danger'); return; }
    st.loading = true; st.error = '';
    renderResult(c);
    try {
      st.result = await api.free({ from: st.from, to: st.to, count: st.count, protocol: st.protocol });
    } catch (e) { st.error = e.message; }
    st.loading = false;
    renderResult(c);
  }

  function renderResult(c) {
    resultBox.textContent = '';
    if (st.loading) { resultBox.appendChild(Card(h('div', { style: { fontSize: 'var(--pl-font-sm)', color: 'var(--pl-n-600)' } }, '查询中…'))); return; }
    if (st.error) { resultBox.appendChild(h('div.pl-inline-error', null, h('span', null, st.error))); return; }
    if (!st.result) { resultBox.appendChild(Card(EmptyState({ title: '尚未查询', desc: '设定范围与数量后点击「查询」' }))); return; }

    const r = st.result;
    const ports = filterPorts(r, st.contiguous, st.count);

    resultBox.appendChild(Card([
      h('div', { style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: '8px' } },
        h('div.pl-card-title', { style: { margin: 0 } }, `推荐端口（${ports.length}）`),
        h('span', { style: { fontSize: 'var(--pl-font-xs)', color: 'var(--pl-n-400)' } }, r.based_on ? `基于上次扫描 ${fmtTime(r.based_on)}` : '尚未扫描')),
      h('div', { style: { display: 'flex', gap: '6px', flexWrap: 'wrap', margin: '12px 0' } },
        ...ports.map(p => PortPicker(p, { onReserve: () => reserve(c, p), onCopy: () => copyOne(p) }))),
      h('div', { style: { display: 'flex', gap: '8px', flexWrap: 'wrap' } },
        Button('复制端口列表', { onClick: () => copy(ports.join(', '), '端口列表') }),
        Button('复制 -p 片段', { onClick: () => copy(ports.map(p => `-p ${p}:${p}`).join('\n'), '端口映射片段') }),
        Button('全部保留', { variant: 'primary', onClick: () => reserve(c, ports) }),
      ),
      ports.length === 0 ? h('div.pl-field-hint', null, '当前条件下没有可用端口，可扩大范围后重试。') : null,
      r.truncated ? h('div.pl-field-hint', null, '结果已截断，仅显示前若干条。') : null,
    ]));

    const ranges = r.ranges || [];
    if (ranges.length) {
      resultBox.appendChild(Card([
        h('div.pl-card-title', null, `空闲区间（${ranges.length} 段）`),
        h('div', { style: { display: 'flex', flexWrap: 'wrap', gap: '6px', maxHeight: '220px', overflow: 'auto' } },
          ...ranges.slice(0, 400).map(g => h('span.pl-tag', null, `${g.from}${g.to > g.from ? '–' + g.to : ''}`, h('span', { style: { color: 'var(--pl-n-400)' } }, ` · ${g.count}`)))),
      ]));
    }
  }

  function filterPorts(r, contiguous, n) {
    if (!contiguous) return (r.ports || []).slice(0, n);
    const g = (r.ranges || []).find(x => x.count >= n);
    if (!g) return (r.ports || []).slice(0, n);
    return Array.from({ length: n }, (_, i) => g.from + i);
  }

  function copy(text, label) {
    if (navigator.clipboard && window.isSecureContext) {
      navigator.clipboard.writeText(text).then(() => toast(`已复制${label}`)).catch(() => fallback());
    } else fallback();
    function fallback() {
      const ta = document.createElement('textarea');
      ta.value = text; document.body.appendChild(ta); ta.select();
      try { document.execCommand('copy'); toast(`已复制${label}`); }
      catch { toast('剪贴板不可用，请手动选择复制', 'warning'); }
      ta.remove();
    }
  }
  function copyOne(p) { copy(`-p ${p}:${p}`, '端口映射'); }
  async function reserve(c, ports) {
    const list = Array.isArray(ports) ? ports : [ports];
    const { openReservationForm } = await import('./reservations.js');
    openReservationForm(c, { ports: list.map(p => ({ port: p, protocol: st.protocol })) });
  }

  renderResult(ctx);
  return wrap;
}
