// P2 端口列表（核心页）+ P3 命名抽屉
// 模式：P-A（列表）+ P-B（表单，抽屉承载）
import { h, fmtClock, fmtTime } from '../lib/dom.js';
import { api } from '../lib/api.js';
import {
  Button, Card, Chip, Field, Input, Textarea, Select, TagInput, Table,
  EmptyState, Pagination, openDrawer, confirmDialog, toast, Badge, Checkbox,
} from '../components/primitives.js';
import { PortBadge, PortNumber, SourceLabel, NameCell, KNOWN_STATES, STATE_LABEL } from '../components/domain.js';

let state = null;

function initState(ctx) {
  const q = new URLSearchParams(location.search);
  state = {
    q: q.get('q') || '',
    states: (q.get('state') || '').split(',').filter(Boolean),
    protocol: q.get('protocol') || '',
    source: q.get('source') || '',
    project: q.get('project') || '',
    page: Number(q.get('page') || 1) || 1,
    page_size: 100,
    rows: [], total: 0, counts: {}, last: null,
    loading: true, error: '', selected: new Set(),
  };
}

function syncURL() {
  const p = new URLSearchParams();
  if (state.q) p.set('q', state.q);
  if (state.states.length) p.set('state', state.states.join(','));
  if (state.protocol) p.set('protocol', state.protocol);
  if (state.source) p.set('source', state.source);
  if (state.project) p.set('project', state.project);
  if (state.page > 1) p.set('page', state.page);
  const qs = p.toString();
  history.replaceState(null, '', '/ports' + (qs ? '?' + qs : ''));
}

async function load(ctx) {
  state.loading = true; state.error = '';
  try {
    const res = await api.ports({
      q: state.q, state: state.states, protocol: state.protocol,
      source: state.source, project: state.project, page: state.page, page_size: state.page_size,
    });
    state.rows = res.items || [];
    state.total = res.total || 0;
    state.counts = res.counts || {};
    state.last = res.last_scan || null;
    state.selected.clear();
  } catch (e) {
    state.error = e.message;
  } finally {
    state.loading = false;
  }
  syncURL();
}

export async function render(ctx) {
  if (!state || state._fresh) { initState(ctx); }
  state._fresh = true;
  await load(ctx);

  const wrap = h('div');

  // A 页头
  wrap.appendChild(h('div.pl-page-head', null,
    h('h1', null, '端口列表', h('span.pl-count-label', null, `共 ${state.total} 条`)),
    h('div', { style: { display: 'flex', gap: '8px', alignItems: 'center' } },
      state.last ? h('span', { style: { fontSize: 'var(--pl-font-xs)', color: 'var(--pl-n-400)' } }, `上次扫描 ${fmtClock(state.last.finished_at)}`) : null,
      Button('导出 CSV', { variant: 'default', onClick: () => exportCSV() }),
      Button('重新扫描', { onClick: async () => { await ctx.scan(); await load(ctx); ctx.rerender(); } }),
    )));

  // B 筛选栏
  const toolbar = h('div.pl-toolbar');
  const search = Input({ value: state.q, placeholder: '搜索端口 / 名称 / 容器 / 项目', ariaLabel: '搜索端口' });
  let timer;
  search.addEventListener('input', () => {
    clearTimeout(timer);
    timer = setTimeout(async () => { state.q = search.value.trim(); state.page = 1; await load(ctx); ctx.rerender(); }, 250);
  });
  search.style.maxWidth = '220px';
  toolbar.appendChild(search);

  for (const st of KNOWN_STATES) {
    const on = state.states.includes(st);
    toolbar.appendChild(Chip(STATE_LABEL[st], {
      pressed: on,
      onClick: async () => {
        if (on) state.states = state.states.filter(x => x !== st); else state.states.push(st);
        state.page = 1; await load(ctx); ctx.rerender();
      },
    }));
  }
  const protoSel = Select([
    { value: '', label: '协议：全部' }, { value: 'tcp', label: 'TCP' }, { value: 'udp', label: 'UDP' },
  ], { value: state.protocol, onChange: async (e) => { state.protocol = e.target.value; state.page = 1; await load(ctx); ctx.rerender(); }, ariaLabel: '协议筛选' });
  protoSel.style.width = '110px';
  toolbar.appendChild(protoSel);

  const srcSel = Select([
    { value: '', label: '来源：全部' },
    { value: 'dict', label: '系统字典' }, { value: 'docker', label: 'Docker 容器' },
    { value: 'proc', label: '宿主进程' }, { value: 'compose', label: 'Compose' },
    { value: 'rproxy', label: '反向代理' },
    { value: 'reserve', label: '保留' }, { value: 'self', label: '本工具' },
  ], { value: state.source, onChange: async (e) => { state.source = e.target.value; state.page = 1; await load(ctx); ctx.rerender(); }, ariaLabel: '来源筛选' });
  srcSel.style.width = '130px';
  toolbar.appendChild(srcSel);

  const projects = [...new Set(state.rows.map(r => r.project).filter(Boolean))].sort();
  if (projects.length) {
    const ps = Select([{ value: '', label: '项目：全部' }, ...projects.map(p => ({ value: p, label: p }))],
      { value: state.project, onChange: async (e) => { state.project = e.target.value; state.page = 1; await load(ctx); ctx.rerender(); }, ariaLabel: '项目筛选' });
    ps.style.width = '130px';
    toolbar.appendChild(ps);
  }

  toolbar.appendChild(Button('重置', { variant: 'text', onClick: async () => {
    state.q = ''; state.states = []; state.protocol = ''; state.source = ''; state.project = ''; state.page = 1;
    await load(ctx); ctx.rerender();
  } }));
  wrap.appendChild(toolbar);

  // C 批量操作条
  if (state.selected.size > 0) {
    const bar = h('div', { style: { display: 'flex', gap: '12px', alignItems: 'center', background: 'var(--pl-brand-50)', padding: '8px 12px', borderRadius: 'var(--pl-radius-md)', marginBottom: '12px' } },
      h('span', { style: { fontSize: 'var(--pl-font-sm)' } }, `已选 ${state.selected.size} 项`),
      Button('批量保留', { variant: 'primary', onClick: () => batchReserve(ctx) }),
      Button('取消选择', { variant: 'text', onClick: () => { state.selected.clear(); ctx.rerender(); } }),
    );
    wrap.appendChild(bar);
  }

  if (state.error) {
    wrap.appendChild(h('div.pl-inline-error', null, h('span', null, '加载失败：' + state.error), Button('重试', { variant: 'text', onClick: async () => { await load(ctx); ctx.rerender(); } })));
  }

  // D 表格
  const columns = [
    { title: '', width: '34px', render: r => {
      const cb = Checkbox({ checked: state.selected.has(key(r)) , ariaLabel: `选择端口 ${r.port}` });
      cb.disabled = r.state !== 'free';
      if (cb.disabled) cb.title = '仅空闲端口可批量保留';
      cb.addEventListener('change', () => { if (cb.checked) state.selected.add(key(r)); else state.selected.delete(key(r)); ctx.rerender(); });
      return cb;
    } },
    { title: '端口', width: '70px', render: r => PortNumber(r.port) },
    { title: '协议', width: '56px', render: r => r.protocol.toUpperCase() },
    { title: '状态', width: '120px', render: r => PortBadge(r) },
    { title: '名称', render: r => NameCell(r) },
    { title: '来源', width: '190px', render: r => SourceLabel(r) },
    { title: '归属项目', width: '110px', render: r => r.project || '—' },
    { title: '最近观测', width: '80px', render: r => r.observed_at ? fmtClock(r.observed_at) : '—' },
    { title: '操作', width: '150px', render: r => h('div.pl-row-actions', null,
      Button('命名', { variant: 'text', onClick: () => openNameDrawer(ctx, r) }),
      Button('详情', { variant: 'text', onClick: () => openNameDrawer(ctx, r, true) }),
      r.state === 'free' ? Button('保留', { variant: 'text', onClick: () => reserveOne(ctx, r) }) : null,
    ) },
  ];

  wrap.appendChild(Table({
    columns, rows: state.rows, loading: state.loading,
    onRowClick: (r) => openNameDrawer(ctx, r),
    empty: EmptyState({
      title: state.total === 0 && !state.q && !state.states.length ? '尚未扫描' : '没有符合条件的端口',
      desc: state.total === 0 && !state.q && !state.states.length ? '执行一次扫描以建立端口台账' : '试试重置筛选条件',
      action: state.total === 0 && !state.q && !state.states.length
        ? Button('重新扫描', { variant: 'primary', onClick: async () => { await ctx.scan(); await load(ctx); ctx.rerender(); } })
        : Button('重置筛选', { variant: 'text', onClick: async () => { state.q = ''; state.states = []; state.protocol = ''; state.source = ''; state.project = ''; state.page = 1; await load(ctx); ctx.rerender(); } }),
    }),
  }));

  wrap.appendChild(Pagination({ page: state.page, pageSize: state.page_size, total: state.total, onChange: async (p) => { state.page = p; await load(ctx); ctx.rerender(); } }));

  return wrap;
}

function key(r) { return `${r.port}/${r.protocol}`; }

function exportCSV() {
  const cols = ['port', 'protocol', 'state', 'state_label', 'name', 'name_source', 'source', 'source_kind', 'project', 'container_name', 'image', 'process_name', 'pid', 'bind_addr', 'conflict'];
  const head = cols.join(',');
  const esc = v => `"${String(v ?? '').replace(/"/g, '""')}"`;
  const lines = state.rows.map(r => cols.map(c => esc(r[c])).join(','));
  const blob = new Blob(['\ufeff' + [head, ...lines].join('\n')], { type: 'text/csv;charset=utf-8' });
  const a = document.createElement('a');
  a.href = URL.createObjectURL(blob);
  a.download = `port-ledger-ports-${new Date().toISOString().slice(0, 10)}.csv`;
  a.click();
  setTimeout(() => URL.revokeObjectURL(a.href), 2000);
}

/* ---------- P3 命名抽屉 ---------- */
export function openNameDrawer(ctx, item, readOnly = false) {
  const tags = (item.tags || '').split(',').map(s => s.trim()).filter(Boolean);
  const nameI = Input({ value: item.name_source === 'user' ? item.name : '', placeholder: item.dict_name || '自定义名称' });
  const noteI = Textarea({ value: item.note || '', maxlength: 200, placeholder: '用途说明，最多 200 字' });
  const projI = Input({ value: item.project || '', placeholder: '归属项目，如 portainer' });
  const tagBox = TagInput({ tags, onChange: () => {} });
  const countEl = h('div.pl-count', null, `${(item.note || '').length}/200`);
  noteI.addEventListener('input', () => { countEl.textContent = `${noteI.value.length}/200`; });

  const info = h('dl.pl-kv');
  const rows = [
    ['状态', h('span', null, PortBadge(item))],
    ['来源', item.source || '—'],
    ['绑定地址', item.bind_addr || '—'],
    ['容器', item.container_name || '—'],
    ['镜像', item.image || '—'],
    ['进程', item.process_name ? `${item.process_name}${item.pid ? ' (PID ' + item.pid + ')' : ''}` : '—'],
    ['命令行', item.cmdline || '—'],
    ['compose', item.compose_file || '—'],
    ['系统字典', item.dict_name ? `${item.dict_name}${item.dict_category ? '（' + item.dict_category + '）' : ''}` : '—'],
    ['扫描时间', item.observed_at ? fmtTime(item.observed_at) : '—'],
  ];
  for (const [k, v] of rows) info.appendChild(h('div', null, h('dt', null, k), h('dd', null, v)));

  const body = h('div', null, info);

  const canRestore = item.name_source === 'user' && !!item.dict_name;
  if (item.state === 'system') {
    body.prepend(h('div.pl-inline-warning', { style: { marginBottom: '12px' } }, '这是群晖系统端口，名称已由你覆盖；状态徽标仍为「系统」。'));
  }
  if (item.conflict) {
    body.prepend(h('div.pl-inline-error', { style: { marginBottom: '12px' } }, '检测到冲突：该端口属于群晖系统保留端口，同时被容器 / 进程 / compose 占用。'));
  }

  const form = h('div');
  form.appendChild(Field('名称', nameI, { hint: item.name_source === 'auto' ? `当前显示系统名：${item.dict_name}` : null }));
  form.appendChild(Field('备注', noteI));
  form.appendChild(h('div.pl-field', null, h('label', null, '标签'), tagBox, h('div.pl-field-hint', null, '回车添加，最多 8 个')));
  form.appendChild(Field('归属项目', projI));
  if (canRestore) {
    form.appendChild(h('div', { style: { marginBottom: '12px' } }, Button('回退系统名', { variant: 'text', onClick: () => { nameI.value = ''; toast('已清空自定义名称，保存后生效', 'warning'); } })));
  }
  body.appendChild(h('div', { style: { borderTop: 'var(--pl-border)', margin: '16px 0', paddingTop: '16px' } },
    h('div.pl-card-title', null, readOnly ? '台账信息' : '台账编辑'),
    form));

  const footer = readOnly ? [Button('关闭', { variant: 'text', onClick: () => d.close() })] : [
    Button('取消', { variant: 'text', onClick: () => d.close() }),
    Button('保存', { variant: 'primary', onClick: async (e) => {
      const btn = e.currentTarget;
      const name = nameI.value.trim();
      if (name.length > 60) { toast('名称不得超过 60 字', 'danger'); return; }
      if (noteI.value.length > 200) { toast('备注不得超过 200 字', 'danger'); return; }
      btn.setAttribute('aria-busy', 'true'); btn.disabled = true;
      try {
        await api.putPort(item.port, {
          protocol: item.protocol, name, note: noteI.value.trim(),
          tags: tags.join(','), project: projI.value.trim(),
        });
        toast(`已保存端口 ${item.port} 的名称`);
        d.close();
        await load(ctx);
        ctx.rerender();
      } catch (err) {
        toast('保存失败：' + err.message, 'danger', { retry: () => btn.click() });
        btn.disabled = false; btn.removeAttribute('aria-busy');
      }
    } }),
  ];

  const d = openDrawer({
    title: `端口 ${item.port} · ${item.protocol.toUpperCase()}`,
    subtitle: item.name || item.dict_name || '',
    body, footer,
  });
  return d;
}

/* ---------- 保留 ---------- */
async function reserveOne(ctx, item) {
  const { openReservationForm } = await import('./reservations.js');
  openReservationForm(ctx, { port: item.port, protocol: item.protocol });
}

async function batchReserve(ctx) {
  const ports = [...state.selected].map(k => { const [p, proto] = k.split('/'); return { port: Number(p), protocol: proto }; });
  const { openReservationForm } = await import('./reservations.js');
  openReservationForm(ctx, { ports });
}
