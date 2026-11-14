// P8 设置 —— 模式 P-B（表单）+ P-E（确认）
import { h, fmtTime } from '../lib/dom.js';
import { api, readFileAsText, downloadText } from '../lib/api.js';
import { Button, Card, Field, Input, Switch, confirmDialog, toast, Badge } from '../components/primitives.js';

let st = { data: null, health: null, loading: true, error: '' };

async function load() {
  st.loading = true; st.error = '';
  try {
    const [s, hh] = await Promise.all([api.settings(), api.health()]);
    st.data = s; st.health = hh;
  } catch (e) { st.error = e.message; }
  st.loading = false;
}

export async function render(ctx) {
  await load();
  const wrap = h('div');
  wrap.appendChild(h('div.pl-page-head', null, h('h1', null, '设置')));

  if (st.error) {
    wrap.appendChild(h('div.pl-inline-error', null, h('span', null, '加载失败：' + st.error), Button('重试', { variant: 'text', onClick: async () => { await load(); ctx.rerender(); } })));
    return wrap;
  }
  if (!st.data) { wrap.appendChild(Card('加载中…')); return wrap; }

  const v = st.data.values || {};
  const env = st.data.env_defaults || {};
  const restartKeys = st.data.restart_required || [];

  const rangesI = Input({ value: v.scan_ranges ?? '', placeholder: '如 1-65535 或 1-1024,8000-8100' });
  const intervalI = Input({ type: 'number', value: v.scan_interval ?? 300, min: 0, max: 86400 });
  const onStartS = Switch({ checked: !!v.scan_on_start });
  const hostProcS = Switch({ checked: !!v.scan_host_proc });
  const dockerSockS = Switch({ checked: !!v.scan_docker_socket });
  const composeDirI = Input({ value: v.scan_compose_dir ?? '', placeholder: '/compose' });
  const tlsS = Switch({ checked: !!v.tls_self_signed });

  const changed = new Set();
  const track = (el, key) => el.addEventListener('change', () => changed.add(key));
  [rangesI, intervalI, composeDirI].forEach(el => el.addEventListener('input', () => changed.add(el === rangesI ? 'scan_ranges' : el === intervalI ? 'scan_interval' : 'scan_compose_dir')));
  track(onStartS.querySelector('input'), 'scan_on_start');
  track(hostProcS.querySelector('input'), 'scan_host_proc');
  track(dockerSockS.querySelector('input'), 'scan_docker_socket');
  track(tlsS.querySelector('input'), 'tls_self_signed');

  // 扫描设置
  wrap.appendChild(Card([
    h('div.pl-card-title', null, '扫描设置'),
    h('div.pl-grid2', null,
      Field('扫描范围', rangesI, { hint: '逗号分隔，支持区间；影响字典与空闲端口判定' }),
      Field('扫描周期（秒）', intervalI, { hint: '0 表示仅手动扫描' }),
    ),
    h('div.pl-field', null, onStartS, h('span', { style: { fontSize: 'var(--pl-font-sm)', color: 'var(--pl-n-600)' } }, ' 首次启动自动全量扫描')),
  ]));

  // 数据源
  wrap.appendChild(Card([
    h('div.pl-card-title', null, '数据源'),
    h('div.pl-field', null, hostProcS, h('span', { style: { fontSize: 'var(--pl-font-sm)', color: 'var(--pl-n-600)' } }, ' 宿主监听表（只读挂载 /host/proc）')),
    h('div.pl-field', null, dockerSockS, h('span', { style: { fontSize: 'var(--pl-font-sm)', color: 'var(--pl-n-600)' } }, ' Docker API（只读挂载 docker.sock）')),
    Field('Compose 目录', composeDirI, { hint: '只读挂载，如 /compose' }),
  ]));

  // 服务（环境变量注入，只读）
  wrap.appendChild(Card([
    h('div.pl-card-title', null, '服务'),
    Field('容器内监听端口', Input({ value: env.admin_port ?? '', disabled: true }), { hint: '由环境变量 ADMIN_PORT 注入，修改请调整 compose' }),
    h('div.pl-field', null, tlsS,
      h('span', { style: { fontSize: 'var(--pl-font-sm)', color: 'var(--pl-n-600)' } }, ' 启用自签证书（HTTPS）'),
      h('div.pl-field-hint', null, '该开关由环境变量决定：此处保存的是期望值，需在 compose 中设置 TLS_SELF_SIGNED 并重启容器后生效。默认明文 HTTP，建议由群晖反向代理终结 TLS。')),
  ]));

  // 保存
  const saveRow = h('div', { style: { display: 'flex', gap: '8px', alignItems: 'center', marginBottom: '20px' } },
    Button('保存设置', { variant: 'primary', onClick: async (e) => {
      if (changed.size === 0) { toast('没有需要保存的修改', 'warning'); return; }
      const btn = e.currentTarget; btn.disabled = true; btn.setAttribute('aria-busy', 'true');
      const payload = {};
      if (changed.has('scan_ranges')) payload.scan_ranges = rangesI.value.trim();
      if (changed.has('scan_interval')) payload.scan_interval = Number(intervalI.value);
      if (changed.has('scan_on_start')) payload.scan_on_start = onStartS.querySelector('input').checked;
      if (changed.has('scan_host_proc')) payload.scan_host_proc = hostProcS.querySelector('input').checked;
      if (changed.has('scan_docker_socket')) payload.scan_docker_socket = dockerSockS.querySelector('input').checked;
      if (changed.has('scan_compose_dir')) payload.scan_compose_dir = composeDirI.value.trim();
      if (changed.has('tls_self_signed')) payload.tls_self_signed = tlsS.querySelector('input').checked;
      try {
        const res = await api.putSettings(payload);
        const needRestart = (res.restart_required || []).length > 0;
        toast(needRestart ? '设置已保存；部分设置需重启容器后生效' : '设置已保存', needRestart ? 'warning' : 'success');
        changed.clear();
        await load(); ctx.rerender();
      } catch (err) { toast('保存失败：' + err.message, 'danger'); btn.disabled = false; btn.removeAttribute('aria-busy'); }
    } }),
    Button('恢复默认', { variant: 'text', onClick: async () => {
      const ok = await confirmDialog({ title: '恢复默认设置', message: '将清除库内设置，回退到环境变量提供的默认值。是否继续？', confirmText: '恢复默认' });
      if (!ok) return;
      try {
        await api.resetSettings();
        toast('已恢复默认（回退环境变量值）');
        await load(); ctx.rerender();
      } catch (e) { toast('恢复失败：' + e.message, 'danger'); }
    } }),
    h('span.pl-hint-row', null, changed.size ? `未保存修改：${changeDesc(changed)}` : '无未保存修改'),
  );
  wrap.appendChild(saveRow);

  // 备份与恢复
  const fileI = h('input', { type: 'file', accept: '.json,application/json', style: { display: 'none' } });
  wrap.appendChild(Card([
    h('div.pl-card-title', null, '备份与恢复'),
    h('div', { style: { fontSize: 'var(--pl-font-sm)', color: 'var(--pl-n-600)', marginBottom: '12px' } },
      '导出台账（命名 / 保留 / 字典覆盖 / 设置），导出文件同时写入 /data/backups，便于群晖 Hyper Backup 覆盖。'),
    h('div', { style: { display: 'flex', gap: '8px', flexWrap: 'wrap' } },
      Button('导出台账 JSON', { variant: 'primary', onClick: () => { location.href = api.backupExportURL(); } }),
      Button('导入台账 JSON', { onClick: () => fileI.click() }),
    ),
    fileI,
  ]));

  fileI.addEventListener('change', async () => {
    const f = fileI.files && fileI.files[0];
    if (!f) return;
    try {
      const data = JSON.parse(await readFileAsText(f));
      const pv = await api.backupPreview(data);
      const ok = await confirmDialog({
        title: '确认导入台账',
        message: `mode=${pv.mode}：将导入 ${pv.ports} 条命名、${pv.reservations} 条保留、${pv.dict_user} 条自定义字典、${pv.dict_overrides} 条覆盖、${pv.settings} 项设置。导入将覆盖同名端口的命名与保留设置。是否继续？`,
        confirmText: '导入',
      });
      if (!ok) { fileI.value = ''; return; }
      await api.backupImport({ mode: 'merge', ...data });
      toast('台账导入完成');
      await load(); ctx.rerender();
    } catch (e) { toast('导入失败：' + e.message, 'danger'); }
    fileI.value = '';
  });

  // 系统信息
  const hh = st.health || {};
  wrap.appendChild(Card([
    h('div.pl-card-title', null, '系统信息'),
    h('dl.pl-kv', null,
      h('div', null, h('dt', null, '版本'), h('dd', null, hh.version || '—')),
      h('div', null, h('dt', null, '运行时长'), h('dd', null, fmtUptime(hh.uptime_seconds))),
      h('div', null, h('dt', null, '字典条目'), h('dd', null, String(hh.dict_size ?? '—'))),
      h('div', null, h('dt', null, '上次扫描'), h('dd', null, hh.last_scan ? `${fmtTime(hh.last_scan.finished_at)} · ${scanStatusLabel(hh.last_scan.status)}` : '尚未扫描')),
      h('div', null, h('dt', null, '扫描次数'), h('dd', null, String(hh.scan_count ?? '—'))),
      h('div', null, h('dt', null, '运行模式'), h('dd', null, (env.tls_self_signed ? 'HTTPS（自签）' : 'HTTP（明文，建议反代终结 TLS）'))),
    ),
  ]));

  return wrap;
}

function changeDesc(set) { return [...set].map(k => ({ scan_ranges: '扫描范围', scan_interval: '扫描周期', scan_on_start: '首启扫描', scan_host_proc: '宿主监听表', scan_docker_socket: 'Docker API', scan_compose_dir: 'Compose 目录', tls_self_signed: 'TLS 自签' }[k] || k)).join('、'); }

function scanStatusLabel(s) { return { ok: '正常', partial: '部分完成', failed: '失败', running: '进行中' }[s] || s; }

function fmtUptime(s) {
  if (!s) return '—';
  const d = Math.floor(s / 86400), hh = Math.floor((s % 86400) / 3600), mm = Math.floor((s % 3600) / 60);
  if (d > 0) return `${d} 天 ${hh} 小时`;
  if (hh > 0) return `${hh} 小时 ${mm} 分`;
  return `${mm} 分`;
}
