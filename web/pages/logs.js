// P7 审计与日志 —— 双 Tab，模式 P-A（列表）
import { h, fmtTime } from '../lib/dom.js';
import { api } from '../lib/api.js';
import { Button, Card, Chip, Input, Select, Table, EmptyState, Tabs, Pagination, Badge, toast } from '../components/primitives.js';

let st = { tab: 'audit', action: '', q: '', audit: [], logs: [], dates: [], loading: true, error: '', page: 1, pageSize: 100 };

async function load() {
  st.loading = true; st.error = '';
  try {
    if (st.tab === 'audit') {
      const res = await api.audit({ action: st.action, target: st.q, limit: 500 });
      st.audit = res.items || [];
    } else {
      const res = await api.logs({ level: st.action, q: st.q, limit: 500 });
      st.logs = res.items || [];
      st.dates = res.dates || [];
    }
  } catch (e) { st.error = e.message; }
  st.loading = false;
}

export async function render(ctx) {
  await load();
  const wrap = h('div');
  wrap.appendChild(h('div.pl-page-head', null,
    h('h1', null, '审计与日志'),
    h('div', { style: { display: 'flex', gap: '8px' } },
      st.tab === 'audit'
        ? Button('导出 CSV', { onClick: exportAuditCSV })
        : Button('下载日志', { onClick: () => { location.href = api.logsDownloadURL(); } }),
      Button('刷新', { variant: 'primary', onClick: async () => { await load(); ctx.rerender(); } }),
    )));

  wrap.appendChild(Tabs([
    { key: 'audit', label: '操作审计' },
    { key: 'log', label: '应用日志' },
  ], { active: st.tab, onChange: async (k) => { st.tab = k; st.action = ''; st.q = ''; await load(); ctx.rerender(); } }));

  const toolbar = h('div.pl-toolbar');
  if (st.tab === 'audit') {
    for (const [k, label] of [['', '全部动作'], ['rename', '命名'], ['reserve', '保留'], ['unreserve', '释放'], ['dict_override', '字典覆盖'], ['dict_import', '字典导入'], ['setting_update', '设置变更']]) {
      toolbar.appendChild(Chip(label, { pressed: st.action === k, onClick: async () => { st.action = k; await load(); ctx.rerender(); } }));
    }
    const s = Input({ value: st.q, placeholder: '搜索端口 / 对象' });
    s.style.maxWidth = '200px';
    let t; s.addEventListener('input', () => { clearTimeout(t); t = setTimeout(async () => { st.q = s.value.trim(); await load(); ctx.rerender(); }, 250); });
    toolbar.appendChild(s);
  } else {
    const lv = Select([{ value: '', label: '全部级别' }, { value: 'debug', label: 'debug' }, { value: 'info', label: 'info' }, { value: 'warn', label: 'warn' }, { value: 'error', label: 'error' }],
      { value: st.action, onChange: async (e) => { st.action = e.target.value; await load(); ctx.rerender(); } });
    lv.style.width = '120px';
    toolbar.appendChild(lv);
    const s = Input({ value: st.q, placeholder: '搜索日志内容' });
    s.style.maxWidth = '220px';
    let t; s.addEventListener('input', () => { clearTimeout(t); t = setTimeout(async () => { st.q = s.value.trim(); await load(); ctx.rerender(); }, 250); });
    toolbar.appendChild(s);
    if (st.dates.length) {
      const d = Select([{ value: '', label: '扫描明细日期' }, ...st.dates.map(x => ({ value: x, label: x }))],
        { value: '', onChange: async (e) => { if (!e.target.value) return; await showScanLog(e.target.value); } });
      d.style.width = '150px';
      toolbar.appendChild(d);
    }
  }
  wrap.appendChild(toolbar);

  if (st.error) wrap.appendChild(h('div.pl-inline-error', null, h('span', null, st.error), Button('重试', { variant: 'text', onClick: async () => { await load(); ctx.rerender(); } })));

  if (st.tab === 'audit') {
    const rows = st.audit.slice((st.page - 1) * st.pageSize, st.page * st.pageSize);
    wrap.appendChild(Table({
      loading: st.loading, rows,
      columns: [
        { title: '时间', width: '150px', render: r => fmtTime(r.ts) },
        { title: '操作人', width: '90px', render: r => r.actor || '本地' },
        { title: '动作', width: '120px', render: r => Badge({ variant: 'neutral', text: r.action }) },
        { title: '对象', width: '160px', render: r => r.target || '—' },
        { title: '详情', render: r => h('span.pl-ellipsis', { title: r.detail }, r.detail || '—') },
      ],
      empty: EmptyState({ title: '暂无审计记录' }),
    }));
    wrap.appendChild(Pagination({ page: st.page, pageSize: st.pageSize, total: st.audit.length, onChange: (p) => { st.page = p; ctx.rerender(); } }));
  } else {
    const rows = st.logs.slice((st.page - 1) * st.pageSize, st.page * st.pageSize).map(e => ({
      time: e.time, level: e.level, msg: e.msg, fields: e.fields ? JSON.stringify(e.fields) : '',
    }));
    wrap.appendChild(Table({
      loading: st.loading, rows,
      columns: [
        { title: '时间', width: '150px', render: r => fmtTime(r.time) },
        { title: '级别', width: '80px', render: r => Badge({ variant: r.level === 'error' ? 'conflict' : (r.level === 'warn' ? 'warning' : 'neutral'), text: r.level }) },
        { title: '消息', render: r => h('span.pl-ellipsis', { title: r.msg }, r.msg) },
        { title: '字段', width: '260px', render: r => h('span.pl-ellipsis', { title: r.fields, style: { fontFamily: 'var(--pl-font-mono)', fontSize: 'var(--pl-font-xs)' } }, r.fields || '—') },
      ],
      empty: EmptyState({ title: '暂无日志' }),
    }));
    wrap.appendChild(Pagination({ page: st.page, pageSize: st.pageSize, total: st.logs.length, onChange: (p) => { st.page = p; ctx.rerender(); } }));
  }

  return wrap;
}

function exportAuditCSV() {
  const cols = ['ts', 'actor', 'action', 'target', 'detail'];
  const esc = v => `"${String(v ?? '').replace(/"/g, '""')}"`;
  const lines = st.audit.map(r => cols.map(c => esc(r[c])).join(','));
  const blob = new Blob(['\ufeff' + [cols.join(','), ...lines].join('\n')], { type: 'text/csv;charset=utf-8' });
  const a = document.createElement('a');
  a.href = URL.createObjectURL(blob);
  a.download = `port-ledger-audit-${new Date().toISOString().slice(0, 10)}.csv`;
  a.click();
  setTimeout(() => URL.revokeObjectURL(a.href), 2000);
}

async function showScanLog(date) {
  try {
    const res = await api.logs({ date });
    const { openModal } = await import('../components/primitives.js');
    const m = openModal({
      title: `扫描明细 ${date}`,
      width: '640px',
      body: h('pre.pl-pre', null, res.content || '（空）'),
      footer: null,
    });
  } catch (e) { toast('读取失败：' + e.message, 'danger'); }
}
