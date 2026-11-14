// P5 保留管理 —— 模式 P-A（列表）+ P-B（表单）+ P-E（确认）
import { h, fmtTime, remainText } from '../lib/dom.js';
import { api } from '../lib/api.js';
import {
  Button, Card, Chip, Field, Input, Select, Table, EmptyState, Pagination,
  openModal, confirmDialog, toast, Badge,
} from '../components/primitives.js';

let st = { state: '', project: '', q: '', rows: [], loading: true, error: '' };

async function load() {
  st.loading = true; st.error = '';
  try {
    const res = await api.reservations({ state: st.state, project: st.project, q: st.q });
    st.rows = res.items || [];
  } catch (e) { st.error = e.message; }
  st.loading = false;
}

export async function render(ctx) {
  await load();
  const wrap = h('div');
  wrap.appendChild(h('div.pl-page-head', null,
    h('h1', null, '保留管理', h('span.pl-count-label', null, `共 ${st.rows.length} 条`)),
    Button('新建保留', { variant: 'primary', onClick: () => openReservationForm(ctx, {}) })));

  const toolbar = h('div.pl-toolbar');
  for (const [k, label] of [['', '全部'], ['active', '有效'], ['expiring', '即将过期'], ['expired', '已过期']]) {
    toolbar.appendChild(Chip(label, { pressed: st.state === k, onClick: async () => { st.state = k; await load(); ctx.rerender(); } }));
  }
  const search = Input({ value: st.q, placeholder: '搜索端口 / 负责人 / 项目' });
  search.style.maxWidth = '220px';
  let timer;
  search.addEventListener('input', () => { clearTimeout(timer); timer = setTimeout(async () => { st.q = search.value.trim(); await load(); ctx.rerender(); }, 250); });
  toolbar.appendChild(search);
  wrap.appendChild(toolbar);

  if (st.error) wrap.appendChild(h('div.pl-inline-error', null, h('span', null, st.error), Button('重试', { variant: 'text', onClick: async () => { await load(); ctx.rerender(); } })));

  wrap.appendChild(Table({
    loading: st.loading, rows: st.rows,
    columns: [
      { title: '端口', width: '70px', render: r => h('span.pl-cell-port', null, String(r.port)) },
      { title: '协议', width: '56px', render: r => r.protocol.toUpperCase() },
      { title: '负责人', render: r => r.owner || '—' },
      { title: '项目', render: r => r.project || '—' },
      { title: '原因', render: r => h('span.pl-ellipsis', { title: r.reason }, r.reason || '—') },
      { title: '过期时间', width: '110px', render: r => r.expires_at ? fmtTime(r.expires_at) : '长期' },
      { title: '剩余', width: '100px', render: r => r.expired ? Badge({ variant: 'neutral', text: '已过期' }) : Badge({ variant: remainClass(r.expires_at), text: remainText(r.expires_at) }) },
      { title: '操作', width: '150px', render: r => h('div.pl-row-actions', null,
        Button('续期', { variant: 'text', onClick: () => openReservationForm(ctx, { reservation: r }) }),
        Button('释放', { variant: 'danger', onClick: async () => {
          const ok = await confirmDialog({ title: '释放保留', message: `释放端口 ${r.port}/${r.protocol.toUpperCase()} 的保留？释放后该端口将被视为空闲。`, confirmText: '释放' });
          if (!ok) return;
          try { await api.deleteReservation(r.id); toast(`已释放端口 ${r.port} 的保留`); await load(); ctx.rerender(); }
          catch (e) { toast('释放失败：' + e.message, 'danger'); }
        } }),
      ) },
    ],
    empty: EmptyState({ title: '还没有任何保留端口', desc: '可在空闲端口页挑选端口并保留给待部署项目', action: Button('去空闲端口', { variant: 'primary', onClick: () => ctx.navigate('/free') }) }),
  }));

  return wrap;
}

function remainClass(expiresAt) {
  if (!expiresAt) return 'reserved';
  const d = new Date(expiresAt).getTime();
  if (isNaN(d)) return 'reserved';
  return (d - Date.now()) <= 3 * 86400000 ? 'warning' : 'reserved';
}

/* 新建 / 编辑保留（P5 表单，P-B） */
export function openReservationForm(ctx, { port, protocol, ports, reservation } = {}) {
  const isEdit = !!reservation;
  const list = ports && ports.length ? ports : (port ? [{ port, protocol }] : []);
  const portI = Input({ type: 'number', value: isEdit ? reservation.port : (list[0]?.port ?? ''), min: 1, max: 65535 });
  portI.disabled = isEdit || list.length === 1;
  const protoS = Select([{ value: 'tcp', label: 'TCP' }, { value: 'udp', label: 'UDP' }], { value: isEdit ? reservation.protocol : (protocol || list[0]?.protocol || 'tcp') });
  protoS.disabled = isEdit || !!list.length;
  const ownerI = Input({ value: isEdit ? reservation.owner : '', placeholder: '负责人' });
  const projI = Input({ value: isEdit ? reservation.project : '', placeholder: '项目，如 nginx-stack' });
  const reasonI = Input({ value: isEdit ? reservation.reason : '', placeholder: '保留原因' });
  const expI = Input({ type: 'date', value: isEdit ? (reservation.expires_at || '').slice(0, 10) : '' });

  const hint = h('div.pl-field-hint');
  const updateHint = () => {
    if (list.length > 1) { hint.textContent = `将批量保留 ${list.length} 个空闲端口`; return; }
    if (isEdit) { hint.textContent = '修改后立即生效'; return; }
    hint.textContent = '仅空闲端口可保留，已占用端口会被拒绝';
  };
  updateHint();

  const body = h('div', null,
    Field('端口', portI),
    Field('协议', protoS),
    Field('负责人', ownerI),
    Field('项目', projI),
    Field('原因', reasonI),
    Field('过期时间', expI, { hint: '留空表示长期有效' }),
    hint,
  );

  const footer = [
    Button('取消', { variant: 'text', onClick: () => m.close() }),
    Button(isEdit ? '保存' : '确认保留', { variant: 'primary', onClick: async (e) => {
      const btn = e.currentTarget;
      btn.setAttribute('aria-busy', 'true'); btn.disabled = true;
      try {
        if (isEdit) {
          await api.updateReservation({ id: reservation.id, owner: ownerI.value.trim(), project: projI.value.trim(), reason: reasonI.value.trim(), expires_at: expI.value });
          toast(`已更新保留 #${reservation.id}`);
        } else if (list.length > 1) {
          let ok = 0; const fails = [];
          for (const it of list) {
            try { await api.createReservation({ port: it.port, protocol: it.protocol, owner: ownerI.value.trim(), project: projI.value.trim(), reason: reasonI.value.trim(), expires_at: expI.value }); ok++; }
            catch (err) { fails.push(`${it.port}: ${err.message}`); }
          }
          toast(fails.length ? `已保留 ${ok} 个端口，${fails.length} 个失败` : `已保留 ${ok} 个端口`, fails.length ? 'warning' : 'success');
        } else {
          await api.createReservation({ port: Number(portI.value), protocol: protoS.value, owner: ownerI.value.trim(), project: projI.value.trim(), reason: reasonI.value.trim(), expires_at: expI.value });
          toast(`端口 ${portI.value} 已保留`);
        }
        m.close();
        await load();
        ctx.rerender();
      } catch (err) {
        toast('操作失败：' + err.message, 'danger', { retry: () => btn.click() });
        btn.disabled = false; btn.removeAttribute('aria-busy');
      }
    } }),
  ];

  const m = openModal({ title: isEdit ? `编辑保留 #${reservation.id}` : '新建保留', body, footer });
  return m;
}
