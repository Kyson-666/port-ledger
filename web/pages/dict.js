// P6 系统端口字典 —— 模式 P-A（列表）+ P-B（表单）+ P-E（确认）
import { h } from '../lib/dom.js';
import { api, readFileAsText, downloadText } from '../lib/api.js';
import {
  Button, Card, Chip, Field, Input, Select, Table, EmptyState, openModal,
  confirmDialog, toast, Badge,
} from '../components/primitives.js';

let st = { category: '', type: '', q: '', data: null, loading: true, error: '' };

async function load() {
  st.loading = true; st.error = '';
  try {
    st.data = await api.dict({ category: st.category, type: st.type, q: st.q });
  } catch (e) { st.error = e.message; }
  st.loading = false;
}

export async function render(ctx) {
  await load();
  const wrap = h('div');
  const d = st.data || { entries: [], categories: [], total: 0, builtin_count: 0, custom_count: 0, override_count: 0 };

  wrap.appendChild(h('div.pl-page-head', null,
    h('h1', null, '系统端口字典',
      h('span.pl-count-label', null, `内置 ${d.builtin_count} · 覆盖 ${d.override_count} · 自定义 ${d.custom_count}`)),
    h('div', { style: { display: 'flex', gap: '8px', flexWrap: 'wrap' } },
      Button('新增条目', { variant: 'primary', onClick: () => openEntryForm(ctx, {}) }),
      Button('导出 JSON', { onClick: () => { location.href = api.dictExportURL(true); } }),
      Button('导入 JSON', { onClick: () => importJSON(ctx) }),
    )));

  const toolbar = h('div.pl-toolbar');
  toolbar.appendChild(Chip('全部类别', { pressed: st.category === '', onClick: async () => { st.category = ''; await load(); ctx.rerender(); } }));
  for (const c of d.categories) {
    toolbar.appendChild(Chip(`${c.name} ${c.count}`, { pressed: st.category === c.name, onClick: async () => { st.category = c.name; await load(); ctx.rerender(); } }));
  }
  wrap.appendChild(toolbar);

  const t2 = h('div.pl-toolbar');
  for (const [k, label] of [['', '全部类型'], ['builtin', '内置'], ['overridden', '已覆盖'], ['custom', '自定义']]) {
    t2.appendChild(Chip(label, { pressed: st.type === k, onClick: async () => { st.type = k; await load(); ctx.rerender(); } }));
  }
  const search = Input({ value: st.q, placeholder: '搜索端口 / 名称 / 类别' });
  search.style.maxWidth = '220px';
  let timer;
  search.addEventListener('input', () => { clearTimeout(timer); timer = setTimeout(async () => { st.q = search.value.trim(); await load(); ctx.rerender(); }, 250); });
  t2.appendChild(search);
  wrap.appendChild(t2);

  if (st.error) wrap.appendChild(h('div.pl-inline-error', null, h('span', null, st.error)));

  const rows = d.entries || [];
  wrap.appendChild(Table({
    loading: st.loading, rows,
    columns: [
      { title: '端口', width: '110px', render: r => h('span.pl-cell-port', null, r.port_end > r.port ? `${r.port}–${r.port_end}` : String(r.port)) },
      { title: '协议', width: '56px', render: r => r.protocol.toUpperCase() },
      { title: '中文名称', render: r => h('span.pl-ellipsis', { title: r.display_name }, r.display_name) },
      { title: '类别', width: '110px', render: r => r.category },
      { title: '来源', width: '130px', render: r => h('span.pl-ellipsis', { title: r.source_ref }, r.source_ref || '—') },
      { title: '类型', width: '90px', render: r => Badge({ variant: r.type === 'custom' ? 'reserved' : (r.type === 'overridden' ? 'warning' : 'neutral'), text: { builtin: '内置', overridden: '已覆盖', custom: '自定义' }[r.type] || r.type }) },
      { title: '操作', width: '190px', render: r => h('div.pl-row-actions', null,
        r.type === 'custom'
          ? [Button('编辑', { variant: 'text', onClick: () => openEntryForm(ctx, { entry: r }) }),
             Button('删除', { variant: 'danger', onClick: () => removeEntry(ctx, r) })]
          : (r.port_end > r.port
            ? h('span', { style: { fontSize: 'var(--pl-font-xs)', color: 'var(--pl-n-400)' } }, '区间条目不可覆盖')
            : [Button('覆盖', { variant: 'text', onClick: () => openOverrideForm(ctx, r) }),
               r.type === 'overridden' ? Button('恢复内置', { variant: 'text', onClick: () => restore(ctx, r) }) : null,
               Button('查看来源', { variant: 'text', onClick: () => showSource(r) })]),
      ) },
    ],
    empty: EmptyState({ title: '该类别暂无条目' }),
  }));

  return wrap;
}

function openEntryForm(ctx, { entry } = {}) {
  const isEdit = !!entry;
  const portI = Input({ type: 'number', value: entry?.port ?? '', min: 1, max: 65535 });
  const endI = Input({ type: 'number', value: entry?.port_end || '', min: 1, max: 65535 });
  const protoS = Select([{ value: 'tcp', label: 'TCP' }, { value: 'udp', label: 'UDP' }, { value: 'both', label: 'TCP + UDP' }], { value: entry?.protocol || 'tcp' });
  const catI = Input({ value: entry?.category || '自定义', placeholder: '类别，如 我的服务' });
  const nameI = Input({ value: entry?.display_name || '', placeholder: '中文名称' });
  const descI = Input({ value: entry?.description || '', placeholder: '备注（可选）' });

  const body = h('div', null,
    Field('起始端口', portI), Field('结束端口', endI, { hint: '单端口请留空' }),
    Field('协议', protoS), Field('类别', catI), Field('中文名称', nameI), Field('备注', descI));

  const m = openModal({
    title: isEdit ? '编辑自定义条目' : '新增字典条目', body,
    footer: [
      Button('取消', { variant: 'text', onClick: () => m.close() }),
      Button('保存', { variant: 'primary', onClick: async (e) => {
        const btn = e.currentTarget; btn.disabled = true; btn.setAttribute('aria-busy', 'true');
        try {
          const payload = {
            action: isEdit ? 'update' : 'create', id: entry?.id,
            port: Number(portI.value), port_end: Number(endI.value) || 0,
            protocol: protoS.value, category: catI.value.trim(), display_name: nameI.value.trim(), description: descI.value.trim(),
          };
          if (!payload.port) throw new Error('端口不能为空');
          if (!payload.display_name) throw new Error('名称不能为空');
          await api.dictPost(payload);
          toast(isEdit ? '已保存自定义条目' : '已新增字典条目');
          m.close(); await load(); ctx.rerender();
        } catch (err) { toast('保存失败：' + err.message, 'danger'); btn.disabled = false; btn.removeAttribute('aria-busy'); }
      } }),
    ],
  });
}

function openOverrideForm(ctx, r) {
  const nameI = Input({ value: r.display_name, placeholder: '新的中文名称' });
  const descI = Input({ value: r.description || '', placeholder: '备注（可选）' });
  const body = h('div', null,
    h('dl.pl-kv', null,
      h('div', null, h('dt', null, '端口'), h('dd', null, `${r.port}/${r.protocol.toUpperCase()}`)),
      h('div', null, h('dt', null, '类别'), h('dd', null, r.category)),
      h('div', null, h('dt', null, '内置名称'), h('dd', null, r.display_name)),
    ),
    h('div', { style: { marginTop: '12px' } }, Field('覆盖名称', nameI), Field('备注', descI)),
    h('div.pl-field-hint', null, '端口、协议与类别只读，避免改动字典主键语义。'));
  const m = openModal({
    title: '覆盖内置名称', body,
    footer: [
      Button('取消', { variant: 'text', onClick: () => m.close() }),
      Button('保存', { variant: 'primary', onClick: async (e) => {
        const btn = e.currentTarget; btn.disabled = true; btn.setAttribute('aria-busy', 'true');
        try {
          await api.dictPost({ action: 'override', port: r.port, protocol: r.protocol, display_name: nameI.value.trim(), description: descI.value.trim() });
          toast('已覆盖内置名称'); m.close(); await load(); ctx.rerender();
        } catch (err) { toast('覆盖失败：' + err.message, 'danger'); btn.disabled = false; btn.removeAttribute('aria-busy'); }
      } }),
    ],
  });
}

async function restore(ctx, r) {
  const ok = await confirmDialog({ title: '恢复内置名称', message: `恢复端口 ${r.port}/${r.protocol.toUpperCase()} 的内置名称？当前覆盖将被删除。`, confirmText: '恢复内置' });
  if (!ok) return;
  try { await api.dictPost({ action: 'restore', port: r.port, protocol: r.protocol }); toast('已恢复内置名称'); await load(); ctx.rerender(); }
  catch (e) { toast('恢复失败：' + e.message, 'danger'); }
}

async function removeEntry(ctx, r) {
  const ok = await confirmDialog({ title: '删除自定义条目', message: `删除自定义字典条目「${r.display_name}」（${r.port}）？该操作不可撤销。`, confirmText: '删除' });
  if (!ok) return;
  try { await api.dictPost({ action: 'delete', id: r.id }); toast('已删除自定义条目'); await load(); ctx.rerender(); }
  catch (e) { toast('删除失败：' + e.message, 'danger'); }
}

function showSource(r) {
  const m = openModal({
    title: `${r.port} · ${r.display_name}`,
    body: h('dl.pl-kv', null,
      h('div', null, h('dt', null, '类别'), h('dd', null, r.category)),
      h('div', null, h('dt', null, '协议'), h('dd', null, r.protocol)),
      h('div', null, h('dt', null, '来源'), h('dd', null, r.source_ref || '—')),
      h('div', null, h('dt', null, '说明'), h('dd', null, r.description || '—'))),
    footer: [Button('关闭', { variant: 'text', onClick: () => m.close() })],
  });
}

function importJSON(ctx) {
  const fileI = h('input', { type: 'file', accept: '.json,application/json' });
  const preview = h('div', { style: { marginTop: '12px' } });
  const body = h('div', null,
    h('div.pl-field-hint', null, '选择由「导出 JSON」生成的字典文件，导入前会先预览变更。'),
    h('div', { style: { marginTop: '8px' } }, fileI), preview);
  const m = openModal({
    title: '导入字典 JSON', body,
    footer: [
      Button('取消', { variant: 'text', onClick: () => m.close() }),
      Button('确认导入', { variant: 'primary', onClick: async (e) => {
        const f = fileI.files && fileI.files[0];
        if (!f) { toast('请先选择文件', 'warning'); return; }
        const btn = e.currentTarget; btn.disabled = true; btn.setAttribute('aria-busy', 'true');
        try {
          const data = JSON.parse(await readFileAsText(f));
          const payload = { user: data.user || [], overrides: data.overrides || [] };
          const pv = await api.dictImportPreview(payload);
          const ok = await confirmDialog({
            title: '确认导入字典',
            message: `将新增 ${pv.new} 条自定义条目，覆盖 ${pv.overrides} 条内置名称，跳过 ${pv.duplicate} 条重复项。是否继续？`,
            confirmText: '导入',
          });
          if (!ok) { btn.disabled = false; btn.removeAttribute('aria-busy'); return; }
          const res = await api.dictImport(payload);
          toast('导入完成');
          m.close(); await load(); ctx.rerender();
        } catch (err) { toast('导入失败：' + err.message, 'danger'); btn.disabled = false; btn.removeAttribute('aria-busy'); }
      } }),
    ],
  });
}
