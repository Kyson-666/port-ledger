// P1 总览 —— 设计系统模式 P-D（统计卡 + 可视化）
import { h } from '../lib/dom.js';
import { api } from '../lib/api.js';
import { Card, Button, Skeleton, EmptyState, Table, Badge } from '../components/primitives.js';
import { Metric, RangeBar, HealthIndicator, PortBadge } from '../components/domain.js';
import { fmtTime, fmtClock } from '../lib/dom.js';

export async function render(ctx) {
  const wrap = h('div');
  const loading = Card(Skeleton({ rows: 4, height: 18 }));
  wrap.appendChild(loading);

  let sum, audit;
  try {
    [sum, audit] = await Promise.all([api.health().then(async (hh) => {
      const s = await api.ports({ page_size: 1 });
      return { health: hh, counts: s.counts, last: s.last_scan };
    }), api.audit({ limit: 5 })]);
  } catch (e) {
    wrap.textContent = '';
    wrap.appendChild(h('div.pl-inline-error', null, h('span', null, '加载失败：' + e.message), Button('重试', { variant: 'text', onClick: () => ctx.rerender() })));
    return wrap;
  }
  wrap.textContent = '';

  const counts = sum.counts || {};
  const occupied = (counts['in-use(docker)'] || 0) + (counts['in-use(host)'] || 0) + (counts['configured'] || 0) + (counts['self'] || 0);
  const health = sum.health.source_health || [];
  const last = sum.last || {};
  const scanning = sum.health.scanning;

  // A 页头
  wrap.appendChild(h('div.pl-page-head', null,
    h('h1', null, '端口总览',
      h('span.pl-count-label', null, last.finished_at ? `上次扫描 ${fmtClock(last.finished_at)}` : '尚未完成扫描')),
    h('div', { style: { display: 'flex', gap: '8px', alignItems: 'center' } },
      HealthIndicator(health),
      Button(scanning ? '扫描中…' : '重新扫描', { variant: 'primary', busy: scanning, disabled: scanning, onClick: async () => { await ctx.scan(); } }),
    )));

  if (scanning) wrap.appendChild(h('div.pl-inline-warning', null, '正在进行首次全量扫描，数据将在完成后自动刷新…'));
  if (health.some(x => x.enabled !== false && !x.ok)) {
    wrap.appendChild(h('div.pl-inline-warning', null, '部分数据源不可用，占用情况可能不完整。'));
  }

  // B 统计卡
  const metrics = h('div.pl-metrics');
  metrics.appendChild(Metric('已占用', occupied, { sub: '容器 / 进程 / 已声明', onClick: () => ctx.navigate('/ports?state=in-use(docker),in-use(host)') }));
  metrics.appendChild(Metric('系统端口', counts['system'] || 0, { sub: '群晖保留，不建议使用', onClick: () => ctx.navigate('/ports?state=system') }));
  metrics.appendChild(Metric('已保留', counts['reserved'] || 0, { sub: '为待部署项目预留', onClick: () => ctx.navigate('/reservations') }));
  metrics.appendChild(Metric('空闲', sum.health ? sum.free_count : '—', { sub: `范围 1024-65535`, onClick: () => ctx.navigate('/free') }));
  wrap.appendChild(metrics);

  // C 端口区间占用条
  wrap.appendChild(Card([
    h('div.pl-card-title', null, '端口分布'),
    ...RangeBar(counts),
    h('div.pl-hint-row', { style: { marginTop: '8px' } }, '点击左侧统计卡可跳转到对应筛选的端口列表。'),
  ]));

  // D 数据源健康
  const healthRows = health.map(x => ({
    name: x.name, enabled: x.enabled !== false, ok: !!x.ok, error: x.error || '', count: x.count || 0, at: x.at,
  }));
  wrap.appendChild(Card([
    h('div.pl-card-title', null, '数据源健康'),
    Table({
      columns: [
        { title: '数据源', key: 'name' },
        { title: '状态', render: r => Badge({ variant: !r.enabled ? 'neutral' : (r.ok ? 'free' : 'conflict'), text: !r.enabled ? '未启用' : (r.ok ? '正常' : '异常') }) },
        { title: '采集数量', render: r => String(r.count) },
        { title: '最近', render: r => fmtClock(r.at) },
        { title: '说明', render: r => r.error || '—' },
      ],
      rows: healthRows,
      empty: EmptyState({ title: '尚无数据源状态', desc: '完成一次扫描后可查看' }),
    }),
  ]));

  // E 最近变动
  const items = (audit && audit.items) || [];
  wrap.appendChild(Card([
    h('div', { style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '12px' } },
      h('div.pl-card-title', { style: { margin: 0 } }, '最近变动'),
      Button('查看全部', { variant: 'text', onClick: () => ctx.navigate('/logs') })),
    items.length === 0
      ? EmptyState({ title: '暂无审计记录', desc: '命名、保留、字典与设置变更都会记录在此' })
      : Table({
        columns: [
          { title: '时间', render: r => fmtTime(r.ts) },
          { title: '动作', render: r => Badge({ variant: 'neutral', text: r.action }) },
          { title: '对象', key: 'target' },
          { title: '详情', render: r => h('span.pl-ellipsis', { title: r.detail }, r.detail || '—') },
          { title: '操作人', key: 'actor' },
        ],
        rows: items,
      }),
  ]));

  return wrap;
}
