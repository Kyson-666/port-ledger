// 前端渲染冒烟测试：在 jsdom 中真实加载页面模块并断言渲染结果。
//
// 运行方式（首次需先装依赖）：
//   npm --prefix scripts install
//   node scripts/frontend-render-test.mjs
import { JSDOM } from 'jsdom';
import { pathToFileURL, fileURLToPath } from 'node:url';
import { resolve, dirname } from 'node:path';

const HERE = dirname(fileURLToPath(import.meta.url));
const WEB = resolve(HERE, '..', 'web');

const dom = new JSDOM('<!doctype html><html><body><div id="app"></div></body></html>', {
  url: 'http://localhost:8899/',
  pretendToBeVisual: true,
});

globalThis.window = dom.window;
globalThis.document = dom.window.document;
Object.defineProperty(globalThis, 'navigator', { value: dom.window.navigator, configurable: true, writable: true });
globalThis.history = dom.window.history;
globalThis.location = dom.window.location;
globalThis.Node = dom.window.Node;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Blob = dom.window.Blob;
globalThis.URL.createObjectURL = () => 'blob:mock';
globalThis.URL.revokeObjectURL = () => {};
window.isSecureContext = false;

/* ---------- 接口桩 ---------- */
const fixtures = {
  '/api/health': {
    status: 'ok', version: '1.0.3', uptime_seconds: 3725, scanning: false, scan_count: 3,
    dict_size: 1843, last_scan: { id: 3, status: 'ok', finished_at: '2026-11-14T21:58:00+08:00', started_at: '2026-11-14T21:57:58+08:00' },
    source_health: [
      { name: 'proc', enabled: true, ok: true, count: 38 },
      { name: 'docker', enabled: true, ok: true, count: 12 },
      { name: 'compose', enabled: true, ok: false, error: 'compose 目录不可用: /compose' },
    ],
    config: { admin_port: 443, scan_interval: 300, scan_ranges: '1-65535' },
  },
  '/api/ports': {
    total: 4, page: 1, page_size: 100,
    counts: { system: 1843, 'in-use(docker)': 12, 'in-use(host)': 8, configured: 3, reserved: 1, free: 0, conflict: 1 },
    last_scan: { id: 3, status: 'ok', finished_at: '2026-11-14T21:58:00+08:00' },
    items: [
      { port: 5000, protocol: 'tcp', state: 'system', state_label: '系统', conflict: false, name: 'DSM 网页 HTTP', name_source: 'auto', source: '系统字典', source_kind: 'dict', observed_at: '2026-11-14T21:58:00+08:00' },
      { port: 6690, protocol: 'tcp', state: 'system', state_label: '系统', conflict: true, name: 'Synology Drive 文件同步与备份', name_source: 'auto', source: '系统字典', source_kind: 'dict', container_name: 'drive' },
      { port: 9000, protocol: 'tcp', state: 'in-use(docker)', state_label: '容器', conflict: false, name: 'Portainer', name_source: 'user', project: 'portainer', source: 'portainer · portainer/portainer-ce', source_kind: 'docker' },
      { port: 25000, protocol: 'tcp', state: 'free', state_label: '空闲', conflict: false, name: '', source: '—', source_kind: '-' },
    ],
  },
  '/api/audit': { total: 1, items: [{ id: 1, ts: '2026-11-14T21:59:00+08:00', actor: '本地', action: 'rename', target: '端口 9000/TCP', detail: '名称 — → Portainer' }] },
  '/api/free': {
    from: 1024, to: 65535, count: 1, protocol: 'tcp', based_on: '2026-11-14T21:58:00+08:00', truncated: true,
    ports: [1024],
    ranges: [{ from: 1024, to: 1400, count: 377 }, { from: 7000, to: 7999, count: 1000 }],
  },
  '/api/settings': {
    values: { scan_ranges: '1-65535', scan_interval: 300, scan_on_start: true, scan_host_proc: true, scan_docker_socket: true, scan_compose_dir: '/compose', admin_port: 443, tls_self_signed: false, webhook: '' },
    env_defaults: { admin_port: 443, scan_ranges: '1-65535' },
    restart_required: ['admin_port', 'tls_self_signed'],
  },
  '/api/reservations': { total: 0, items: [] },
  '/api/system-dict': {
    total: 3, builtin_count: 109, custom_count: 0, override_count: 0,
    categories: [{ name: '系统管理', count: 11 }, { name: '文档同步', count: 3 }],
    entries: [
      { port: 5000, port_end: 0, protocol: 'tcp', category: '系统管理', display_name: 'DSM 网页 HTTP', source_ref: 'Synology KB', type: 'builtin' },
      { port: 6690, port_end: 0, protocol: 'tcp', category: '文档同步', display_name: 'Synology Drive 文件同步与备份', source_ref: 'Synology KB', type: 'builtin' },
      { port: 8500, port_end: 8520, protocol: 'tcp', category: '邮件服务', display_name: 'MailPlus Server', source_ref: 'Synology KB', type: 'builtin' },
    ],
  },
  '/api/logs': { items: [{ time: '2026-11-14T21:58:00+08:00', level: 'info', msg: '扫描完成', fields: { scan_id: 3 } }], dates: ['2026-11-14'], app_log: 'app.log' },
};

const calls = [];
globalThis.fetch = async (url) => {
  const path = String(url).split('?')[0];
  calls.push(String(url));
  const body = fixtures[path];
  if (!body) return { ok: false, status: 404, statusText: 'Not Found', text: async () => '{"error":"not found"}' };
  return { ok: true, status: 200, statusText: 'OK', text: async () => JSON.stringify(body) };
};

/* ---------- 断言工具 ---------- */
let pass = 0, fail = 0;
function check(name, cond, extra = '') {
  if (cond) { pass++; console.log(`  ✓ ${name}`); }
  else { fail++; console.error(`  ✗ ${name} ${extra}`); }
}
const load = (rel) => import(pathToFileURL(resolve(WEB, rel)).href);
const ctx = { navigate: () => {}, rerender: () => {}, scan: async () => {} };

console.log('前端渲染冒烟测试（jsdom）');

{
  console.log('P1 总览');
  const { render } = await load('pages/overview.js');
  const text = (await render(ctx)).textContent;
  check('渲染出标题「端口总览」', text.includes('端口总览'));
  check('四张统计卡齐全', ['已占用', '系统端口', '已保留', '空闲'].every(k => text.includes(k)));
  check('展示上次扫描时间', text.includes('上次扫描'));
  check('端口分布含图例', text.includes('端口分布') && text.includes('系统 1843'));
  check('数据源健康列出三项', ['proc', 'docker', 'compose'].every(k => text.includes(k)));
  check('降级提示可见', text.includes('部分数据源不可用') || text.includes('部分数据可能不全'));
  check('最近变动展示审计', text.includes('最近变动') && text.includes('rename'));
  check('空闲数由 /api/free 区间汇总（非 undefined）', !text.includes('undefined') && text.includes('1377'));
}

{
  console.log('P2 端口列表');
  const { render } = await load('pages/ports.js');
  const node = await render(ctx);
  const text = node.textContent;
  check('渲染出标题「端口列表」', text.includes('端口列表'));
  check('表头含端口/协议/状态/名称', ['端口', '协议', '状态', '名称', '来源'].every(k => text.includes(k)));
  check('系统端口显示自动命名标记', text.includes('自动'));
  check('空闲徽标样式存在', !!node.querySelector('.pl-badge--free') || text.includes('空闲'));
  check('系统徽标带左侧品牌条', !!node.querySelector('.pl-badge--system'));
  check('冲突叠加态渲染', !!node.querySelector('.pl-badge--conflict') && text.includes('冲突'));
  check('容器徽标为中性灰', !!node.querySelector('.pl-badge--docker'));
  check('命名/详情/保留 行内操作存在', text.includes('命名') && text.includes('详情'));
  check('分页显示总数', text.includes('共 4 条'));
  check('触发 /api/ports 请求', calls.some(u => u.startsWith('/api/ports')));
  // 批量选择：勾选空闲行后应出现「已选 N 项」批量条，且勾选态保持
  const cbs = [...node.querySelectorAll('input.pl-checkbox')];
  const usable = cbs.find(c => !c.disabled);
  check('空闲行复选框可用', !!usable);
  if (usable) {
    usable.checked = true;
    usable.dispatchEvent(new dom.window.Event('change'));
    check('勾选后出现批量操作条', node.textContent.includes('已选 1 项'));
  }
  // 同页重渲染不得清空多选与筛选（此前每次 render 都重置 state）
  const again = await render({ ...ctx, rerender: () => {} });
  check('重渲染后保留勾选', again.textContent.includes('已选 1 项'));
}

{
  console.log('P4 空闲端口');
  const { render } = await load('pages/free.js');
  const text = (await render(ctx)).textContent;
  check('渲染查询条件', text.includes('查询条件'));
  check('含范围/数量/协议字段', ['起始端口', '结束端口', '需要数量', '协议'].every(k => text.includes(k)));
  check('未查询时给出引导空态', text.includes('尚未查询'));
}

{
  console.log('P5 保留管理');
  const { render } = await load('pages/reservations.js');
  const text = (await render(ctx)).textContent;
  check('渲染标题与新建入口', text.includes('保留管理') && text.includes('新建保留'));
  check('筛选 chip 齐全', ['全部', '有效', '即将过期', '已过期'].every(k => text.includes(k)));
  check('空态引导去空闲端口', text.includes('还没有任何保留端口'));
}

{
  console.log('P6 系统字典');
  const { render } = await load('pages/dict.js');
  const text = (await render(ctx)).textContent;
  check('渲染标题与统计', text.includes('系统端口字典') && text.includes('内置 109'));
  check('类别树含计数', text.includes('系统管理 11'));
  check('区间条目折叠显示', text.includes('8500–8520'));
  check('导出/导入/新增入口', ['新增条目', '导出 JSON', '导入 JSON'].every(k => text.includes(k)));
}

{
  console.log('P7 审计与日志');
  const { render } = await load('pages/logs.js');
  const text = (await render(ctx)).textContent;
  check('双 Tab 存在', text.includes('操作审计') && text.includes('应用日志'));
  check('审计表格渲染', text.includes('rename') && text.includes('端口 9000/TCP'));
  check('提供导出与刷新', text.includes('导出 CSV') && text.includes('刷新'));
}

{
  console.log('P8 设置');
  const { render } = await load('pages/settings.js');
  const node = await render(ctx);
  const text = node.textContent;
  check('三个分组齐全', ['扫描设置', '数据源', '服务'].every(k => text.includes(k)));
  check('无 webhook 残留', !text.includes('Webhook') && !text.includes('webhook'));
  check('只读项标注环境变量注入', text.includes('ADMIN_PORT'));
  check('TLS 提示需重启', text.includes('重启容器后生效'));
  check('备份与恢复入口', text.includes('备份与恢复') && text.includes('导出台账 JSON'));
  check('系统信息含版本', text.includes('系统信息') && text.includes('1.0.3'));
  // 开关统一使用组件自带 label：文字与滑块同处一个 label，且覆盖全部开关项
  const swLabels = [...node.querySelectorAll('label.pl-switch')].map(l => l.textContent.trim());
  check('数据源开关含「群晖反向代理」', text.includes('群晖反向代理'));
  check('开关文字并入组件 label（不手拼 span）',
    ['首次启动自动全量扫描', '宿主监听表', 'Docker API', '群晖反向代理', '启用自签证书'].every(k => swLabels.some(s => s.includes(k))));
}

console.log(`\n结果：通过 ${pass} 项，失败 ${fail} 项。`);
process.exit(fail ? 1 : 0);
