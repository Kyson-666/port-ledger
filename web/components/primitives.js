// Primitives —— 设计系统 §5.1 的基础组件实现（含统一状态矩阵与全局反馈宿主）
import { h, mount, clear, esc } from '../lib/dom.js';

/* ---------- 全局宿主 ---------- */
let toastHost, overlayHost;
function ensureHosts() {
  if (!toastHost) { toastHost = h('div.pl-toasts', { role: 'status', 'aria-live': 'polite' }); document.body.appendChild(toastHost); }
  if (!overlayHost) { overlayHost = h('div', { id: 'pl-overlays' }); document.body.appendChild(overlayHost); }
}
export function initHosts() { ensureHosts(); }

/* ---------- Toast（全局单例队列） ---------- */
export function toast(message, type = 'success', opts = {}) {
  ensureHosts();
  const el = h('div.pl-toast', { class: type ? `pl-toast--${type}` : '' },
    h('span', { style: { flex: '1' } }, message),
    opts.retry ? h('button', { onClick: () => { el.remove(); opts.retry(); } }, '重试') : null,
    h('button', { onClick: () => el.remove(), 'aria-label': '关闭' }, '✕'),
  );
  toastHost.appendChild(el);
  if (type !== 'danger' && !opts.persist) setTimeout(() => el.remove(), 3000);
  return el;
}

/* ---------- Badge ---------- */
export function Badge({ variant = 'neutral', text, icon }) {
  return h('span.pl-badge', { class: `pl-badge--${variant}` }, icon ? h('span', { 'aria-hidden': 'true' }, icon) : null, text);
}

/* ---------- Button ---------- */
export function Button(label, { variant = 'default', onClick, disabled, type = 'button', title, busy } = {}) {
  const cls = ['pl-btn'];
  if (variant === 'primary') cls.push('pl-btn--primary');
  if (variant === 'text') cls.push('pl-btn--text');
  if (variant === 'danger') cls.push('pl-btn--text', 'pl-btn--danger');
  const el = h('button', {
    class: cls.join(' '), type, title: title || null,
    disabled: disabled || busy || false, 'aria-busy': busy ? 'true' : null,
    onClick,
  }, label);
  return el;
}

/* ---------- 表单控件 ---------- */
export function Field(label, control, { hint, error, count } = {}) {
  return h('div.pl-field', null,
    label ? h('label', null, label) : null,
    control,
    error ? h('div.pl-field-error', null, error) : null,
    count ? h('div.pl-count', null, count) : null,
    hint ? h('div.pl-field-hint', null, hint) : null,
  );
}

export function Input({ value = '', placeholder, onInput, type = 'text', disabled, invalid, ariaLabel, min, max } = {}) {
  return h('input.pl-input', {
    type, value, placeholder: placeholder || null, disabled: disabled || false,
    'aria-invalid': invalid ? 'true' : null, 'aria-label': ariaLabel || null,
    min: min ?? null, max: max ?? null,
    onInput: onInput || null,
  });
}

export function Textarea({ value = '', placeholder, onInput, maxlength, disabled } = {}) {
  return h('textarea.pl-textarea', {
    placeholder: placeholder || null, maxlength: maxlength ?? null, disabled: disabled || false,
    onInput: onInput || null,
  }, value);
}

export function Select(options, { value = '', onChange, disabled, ariaLabel } = {}) {
  const el = h('select.pl-select', { onChange, disabled: disabled || false, 'aria-label': ariaLabel || null });
  for (const o of options) {
    el.appendChild(h('option', { value: o.value, selected: String(o.value) === String(value) }, o.label));
  }
  return el;
}

export function Checkbox({ checked = false, onChange, ariaLabel } = {}) {
  return h('input.pl-checkbox', { type: 'checkbox', checked, onChange, 'aria-label': ariaLabel || null });
}

export function Switch({ checked = false, onChange, label, disabled } = {}) {
  const input = h('input', { type: 'checkbox', checked, onChange, disabled: disabled || false });
  return h('label.pl-switch', null, input, h('span.pl-switch-track'), label ? h('span', null, label) : null);
}

export function Chip(label, { pressed = false, onClick } = {}) {
  return h('button.pl-chip', { type: 'button', 'aria-pressed': pressed ? 'true' : 'false', onClick }, label);
}

export function TagInput({ tags = [], onChange, placeholder = '回车添加标签' } = {}) {
  const box = h('div.pl-taginput');
  const render = () => {
    clear(box);
    tags.forEach((t, i) => box.appendChild(h('span.pl-tag', null, t,
      h('button', { type: 'button', 'aria-label': '删除标签 ' + t, onClick: () => { tags.splice(i, 1); onChange(tags); render(); } }, '✕'))));
    const input = h('input', {
      placeholder, onKeydown: (e) => {
        if (e.key === 'Enter' && input.value.trim()) {
          e.preventDefault();
          const v = input.value.trim();
          if (tags.length >= 8) { toast('标签最多 8 个', 'warning'); return; }
          if (!tags.includes(v)) tags.push(v);
          input.value = '';
          onChange(tags); render();
          box.querySelector('input')?.focus();
        }
        if (e.key === 'Backspace' && !input.value && tags.length) { tags.pop(); onChange(tags); render(); box.querySelector('input')?.focus(); }
      },
    });
    box.appendChild(input);
  };
  render();
  return box;
}

/* ---------- Card / Tabs / Empty / Skeleton ---------- */
export function Card(children, { title } = {}) {
  const el = h('div.pl-card');
  if (title) el.appendChild(h('div.pl-card-title', null, title));
  for (const c of [].concat(children)) if (c) el.appendChild(c);
  return el;
}

export function Tabs(items, { active, onChange } = {}) {
  const bar = h('div.pl-tabs', { role: 'tablist' });
  for (const it of items) {
    bar.appendChild(h('button.pl-tab', {
      role: 'tab', type: 'button',
      'aria-selected': it.key === active ? 'true' : 'false',
      onClick: () => onChange(it.key),
    }, it.label));
  }
  return bar;
}

export function EmptyState({ title, desc, action } = {}) {
  return h('div.pl-empty', null,
    h('div.pl-empty-title', null, title || '暂无数据'),
    desc ? h('div.pl-empty-desc', null, desc) : null,
    action || null,
  );
}

export function Skeleton({ rows = 3, height = 14 } = {}) {
  const box = h('div', { style: { display: 'grid', gap: '8px' } });
  for (let i = 0; i < rows; i++) box.appendChild(h('div.pl-skeleton', { style: { height: height + 'px', width: (100 - i * 8) + '%' } }));
  return box;
}

/* ---------- 分页 ---------- */
export function Pagination({ page, pageSize, total, onChange, unit = '条' }) {
  const pages = Math.max(1, Math.ceil(total / pageSize));
  return h('div.pl-pagination', null,
    h('span', null, `共 ${total} ${unit} · 每页 ${pageSize}`),
    h('span', { style: { display: 'flex', gap: '6px', alignItems: 'center' } },
      Button('上一页', { variant: 'text', disabled: page <= 1, onClick: () => onChange(page - 1) }),
      h('span', null, `第 ${page} / ${pages} 页`),
      Button('下一页', { variant: 'text', disabled: page >= pages, onClick: () => onChange(page + 1) }),
    ),
  );
}

/* ---------- Table（含三态） ---------- */
export function Table({ columns, rows, rowKey, onRowClick, empty, loading, error, onRetry }) {
  const wrap = h('div');
  if (error) {
    wrap.appendChild(h('div.pl-inline-error', null, h('span', null, error), Button('重试', { variant: 'text', onClick: onRetry })));
  }
  if (loading && !rows?.length) {
    wrap.appendChild(Card(Skeleton({ rows: 6, height: 16 })));
    return wrap;
  }
  if (!rows || rows.length === 0) {
    wrap.appendChild(Card(empty || EmptyState({ title: '没有符合条件的端口' })));
    return wrap;
  }
  const table = h('table.pl-table');
  const thead = h('thead');
  const tr = h('tr');
  for (const c of columns) {
    tr.appendChild(h('th', { style: c.width ? { width: c.width } : null, scope: 'col' }, c.title));
  }
  thead.appendChild(tr);
  table.appendChild(thead);

  const tbody = h('tbody');
  for (const r of rows) {
    const row = h('tr', { tabindex: '0', style: { cursor: onRowClick ? 'pointer' : 'default' },
      onClick: (e) => { if (e.target.closest('button,input,a,select')) return; onRowClick && onRowClick(r, e); },
      onKeydown: (e) => { if (e.key === 'Enter' && onRowClick) onRowClick(r, e); },
    });
    for (const c of columns) {
      const td = h('td', { class: c.class || null });
      const v = c.render ? c.render(r) : r[c.key];
      if (v instanceof Node) td.appendChild(v); else td.textContent = v ?? '—';
      row.appendChild(td);
    }
    tbody.appendChild(row);
  }
  table.appendChild(tbody);
  wrap.appendChild(h('div.pl-table-wrap', null, table));

  // <768px 卡片式列表
  const list = h('div.pl-cardlist');
  for (const r of rows) {
    const c = Card(null);
    c.style.cursor = onRowClick ? 'pointer' : 'default';
    if (onRowClick) c.addEventListener('click', (e) => { if (!e.target.closest('button,input,a,select')) onRowClick(r, e); });
    for (const col of columns) {
      const v = col.render ? col.render(r) : r[col.key];
      const line = h('div', { style: { display: 'flex', gap: '8px', padding: '3px 0' } },
        h('span', { style: { color: 'var(--pl-n-400)', minWidth: '64px', fontSize: 'var(--pl-font-xs)' } }, col.title),
        h('span', { style: { flex: '1', minWidth: '0', wordBreak: 'break-all' } }, v instanceof Node ? '' : String(v ?? '—')));
      if (v instanceof Node) line.lastChild.appendChild(v);
      c.appendChild(line);
    }
    list.appendChild(c);
  }
  wrap.appendChild(list);
  return wrap;
}

/* ---------- Drawer / Modal ---------- */
export function openDrawer({ title, subtitle, body, footer, onClose }) {
  ensureHosts();
  const overlay = h('div.pl-overlay');
  const drawer = h('aside.pl-drawer', { role: 'dialog', 'aria-modal': 'true' });
  let closed = false;
  const close = () => {
    if (closed) return; closed = true;
    overlay.remove(); drawer.remove(); document.removeEventListener('keydown', onKey);
    onClose && onClose();
  };
  const onKey = (e) => { if (e.key === 'Escape') close(); };
  document.addEventListener('keydown', onKey);
  overlay.addEventListener('click', close);

  drawer.appendChild(h('div.pl-drawer-head', null,
    h('div', null, h('h2', null, title), subtitle ? h('div', { style: { fontSize: 'var(--pl-font-xs)', color: 'var(--pl-n-400)' } }, subtitle) : null),
    Button('✕', { variant: 'text', onClick: close, title: '关闭' })));
  const bodyEl = h('div.pl-drawer-body', null, body);
  drawer.appendChild(bodyEl);
  if (footer) drawer.appendChild(h('div.pl-drawer-foot', null, footer));

  overlayHost.append(overlay, drawer);
  setTimeout(() => drawer.querySelector('input,textarea,button')?.focus(), 30);
  return { close, body: bodyEl };
}

export function openModal({ title, body, footer, onClose, width }) {
  ensureHosts();
  const overlay = h('div.pl-overlay');
  const modal = h('div.pl-modal', { role: 'dialog', 'aria-modal': 'true' });
  if (width) modal.style.width = width;
  let closed = false;
  const close = () => {
    if (closed) return; closed = true;
    overlay.remove(); modal.remove(); document.removeEventListener('keydown', onKey);
    onClose && onClose();
  };
  const onKey = (e) => { if (e.key === 'Escape') close(); };
  document.addEventListener('keydown', onKey);
  overlay.addEventListener('click', close);
  modal.appendChild(h('div.pl-modal-head', null, title));
  modal.appendChild(h('div.pl-modal-body', null, body));
  if (footer) modal.appendChild(h('div.pl-modal-foot', null, footer));
  overlayHost.append(overlay, modal);
  setTimeout(() => modal.querySelector('input,textarea,select,button')?.focus(), 30);
  return { close };
}

export function confirmDialog({ title = '确认操作', message, confirmText = '确认', danger = true }) {
  return new Promise((resolve) => {
    const { close } = openModal({
      title,
      body: h('div', { style: { fontSize: 'var(--pl-font-sm)' } }, message),
      footer: h('div', { style: { display: 'flex', gap: '8px', justifyContent: 'flex-end' } },
        Button('取消', { variant: 'text', onClick: () => { close(); resolve(false); } }),
        Button(confirmText, { variant: danger ? 'danger' : 'primary', onClick: () => { close(); resolve(true); } })),
      onClose: () => resolve(false),
    });
  });
}
