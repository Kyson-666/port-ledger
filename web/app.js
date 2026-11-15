// PortLedger 管理台入口：外壳、路由、全局反馈
import { h, mount, clear, fmtClock } from './lib/dom.js';
import { api } from './lib/api.js';
import { initHosts, Button, Input, toast } from './components/primitives.js';
import { HealthIndicator } from './components/domain.js';

const ROUTES = [
  { path: '/', label: '总览', group: '概览', mod: () => import('./pages/overview.js') },
  { path: '/ports', label: '端口列表', group: '概览', mod: () => import('./pages/ports.js') },
  { path: '/free', label: '空闲端口', group: '概览', mod: () => import('./pages/free.js') },
  { path: '/reservations', label: '保留管理', group: '管理', mod: () => import('./pages/reservations.js') },
  { path: '/dict', label: '系统字典', group: '管理', mod: () => import('./pages/dict.js') },
  { path: '/logs', label: '审计与日志', group: '系统', mod: () => import('./pages/logs.js') },
  { path: '/settings', label: '设置', group: '系统', mod: () => import('./pages/settings.js') },
];

const app = document.getElementById('app');
let pageHost, healthHost, scanBtn, lastScanHost, topbarSearch;
let currentPath = '/';
let health = null;

initHosts();

function shell() {
  const nav = h('nav.pl-nav', { 'aria-label': '主导航' }, h('div.pl-nav-wrap'));
  const navWrap = nav.querySelector('.pl-nav-wrap');
  navWrap.appendChild(h('div.pl-nav-brand', null, 'PortLedger', h('small', null, '端口台账')));

  const groups = {};
  for (const r of ROUTES) (groups[r.group] = groups[r.group] || []).push(r);
  for (const [g, items] of Object.entries(groups)) {
    const box = h('div.pl-nav-group', null, h('div.pl-nav-group-title', null, g));
    for (const it of items) {
      box.appendChild(h('a.pl-nav-item', {
        href: it.path, 'data-path': it.path,
        onClick: (e) => { e.preventDefault(); navigate(it.path); },
      }, h('span', null, it.label)));
    }
    navWrap.appendChild(box);
  }
  navWrap.appendChild(h('div.pl-nav-foot', null, 'v1.0.3 · 无认证（内网）'));

  // 全局搜索（设计稿 §1.2）：回车跳转端口列表并带筛选条件。
  // 端口列表页内已有「输入即时过滤」搜索框，此时隐藏顶栏这一个，避免同页两个搜索框。
  const search = Input({ placeholder: '搜索端口 / 名称 / 容器 / 项目', ariaLabel: '全局搜索' });
  search.className = 'pl-input pl-search';
  search.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') { const v = search.value.trim(); navigate('/ports' + (v ? '?q=' + encodeURIComponent(v) : '')); }
  });
  // 「/」聚焦当前页可见的搜索框（R-09）：端口列表页聚焦页内过滤框，其余页聚焦全局搜索
  document.addEventListener('keydown', (e) => {
    const tag = document.activeElement && document.activeElement.tagName;
    if (e.key === '/' && tag !== 'INPUT' && tag !== 'TEXTAREA') {
      const pageSearch = document.querySelector('.pl-toolbar input.pl-input');
      const target = search.offsetParent !== null ? search : pageSearch;
      if (target) { e.preventDefault(); target.focus(); }
    }
  });

  healthHost = h('span');
  lastScanHost = h('span', { style: { fontSize: 'var(--pl-font-xs)', color: 'var(--pl-n-400)', whiteSpace: 'nowrap' } });
  scanBtn = Button('重新扫描', { variant: 'primary', onClick: doScan });

  // 「重新扫描」为全站唯一入口（设计稿 §1.3：每页 primary ≤ 1）
  const topbar = h('header.pl-topbar', null,
    search, h('div.pl-topbar-spacer'), lastScanHost, healthHost, scanBtn);
  topbarSearch = search;

  pageHost = h('main.pl-content');
  const main = h('div.pl-main', null, topbar, pageHost);
  mount(app, h('div.pl-shell', null, nav, main));
  return nav;
}

let navEl;
function setActive(path) {
  currentPath = path;
  navEl.querySelectorAll('.pl-nav-item').forEach(a => {
    if (a.dataset.path === location.pathname || a.dataset.path === path) a.setAttribute('aria-current', 'page');
    else a.removeAttribute('aria-current');
  });
  // 端口列表页内已有即时过滤框 → 隐藏顶栏全局搜索，避免同页两个搜索框
  if (topbarSearch) topbarSearch.style.display = path === '/ports' ? 'none' : '';
}

const ctx = {
  navigate,
  rerender: () => renderRoute(),
  scan: doScan,
};

async function doScan() {
  scanBtn.disabled = true; scanBtn.setAttribute('aria-busy', 'true');
  try {
    const res = await api.scan({});
    const n = res.counts ? Object.values(res.counts).reduce((a, b) => a + b, 0) : 0;
    if (res.status === 'partial') toast('扫描完成，部分数据源不可用，结果可能不完整', 'warning');
    else if (res.status === 'failed') toast('扫描失败，已保留上次结果', 'danger');
    else toast(`扫描完成 · 共 ${n} 条端口记录`);
    await refreshHealth();
    await renderRoute();
  } catch (e) {
    toast('扫描失败：' + e.message, 'danger', { retry: doScan });
  } finally {
    scanBtn.disabled = false; scanBtn.removeAttribute('aria-busy');
  }
}

async function refreshHealth() {
  try {
    health = await api.health();
    clear(healthHost);
    healthHost.appendChild(HealthIndicator(health.source_health || []));
    lastScanHost.textContent = health.last_scan ? `上次扫描 ${fmtClock(health.last_scan.finished_at)}` : '尚未扫描';
    if (health.scanning) { scanBtn.disabled = true; scanBtn.setAttribute('aria-busy', 'true'); }
    else { scanBtn.disabled = false; scanBtn.removeAttribute('aria-busy'); }
  } catch { /* 静默 */ }
}

function navigate(path) {
  const [p] = path.split('?');
  const [q] = path.split('?').slice(1);
  const url = path;
  if (location.pathname + location.search !== url) history.pushState(null, '', url);
  setActive(p);
  renderRoute();
}

let renderToken = 0;
async function renderRoute() {
  const path = location.pathname === '' ? '/' : location.pathname;
  const route = ROUTES.find(r => r.path === path) || ROUTES[0];
  setActive(route.path);
  const token = ++renderToken;

  // 骨架
  mount(pageHost, h('div.pl-card', null, h('div.pl-skeleton', { style: { height: '18px', width: '40%' } })));
  try {
    const mod = await route.mod();
    const node = await mod.render(ctx);
    if (token !== renderToken) return; // 已被更新的导航取代
    mount(pageHost, node);
    document.title = `PortLedger · ${route.label}`;
  } catch (e) {
    if (token !== renderToken) return;
    mount(pageHost, h('div.pl-inline-error', null, h('span', null, '页面加载失败：' + e.message), Button('重试', { variant: 'text', onClick: () => renderRoute() })));
  }
}

window.addEventListener('popstate', () => { setActive(location.pathname); renderRoute(); });

navEl = shell();
refreshHealth();
renderRoute();
setInterval(() => { if (!document.hidden) refreshHealth(); }, 30000);
