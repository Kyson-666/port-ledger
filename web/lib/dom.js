// 极简 DOM 构建器：h('div.cls#id', {attrs}, ...children)
export function h(tag, props, ...children) {
  let name = tag, id = '';
  const m = tag.match(/^([a-zA-Z0-9-]+)/);
  name = m ? m[1] : 'div';
  const idm = tag.match(/#([\w-]+)/);
  if (idm) id = idm[1];
  const classes = [...tag.matchAll(/\.([\w-]+)/g)].map(x => x[1]);
  const el = document.createElement(name);
  if (id) el.id = id;
  if (classes.length) el.className = classes.join(' ');

  if (props && typeof props === 'object' && !(props instanceof Node) && !Array.isArray(props)) {
    for (const [k, v] of Object.entries(props)) {
      if (v === null || v === undefined || v === false) continue;
      if (k === 'class' || k === 'className') { el.className = [el.className, v].filter(Boolean).join(' '); continue; }
      if (k === 'style' && typeof v === 'object') { Object.assign(el.style, v); continue; }
      if (k === 'dataset' && typeof v === 'object') { Object.assign(el.dataset, v); continue; }
      if (k.startsWith('on') && typeof v === 'function') { el.addEventListener(k.slice(2).toLowerCase(), v); continue; }
      if (k === 'html') { el.innerHTML = v; continue; }
      if (k === 'text') { el.textContent = v; continue; }
      if (k === 'value' && (name === 'input' || name === 'textarea' || name === 'select')) { el.value = v; continue; }
      if (k === 'checked' || k === 'disabled' || k === 'selected') { el[k] = !!v; continue; }
      el.setAttribute(k, v === true ? '' : String(v));
    }
  } else if (props !== undefined && props !== null) {
    children.unshift(props);
  }

  append(el, children);
  return el;
}

function append(el, children) {
  for (const c of children) {
    if (c === null || c === undefined || c === false || c === true) continue;
    if (Array.isArray(c)) { append(el, c); continue; }
    el.appendChild(c instanceof Node ? c : document.createTextNode(String(c)));
  }
}

export function clear(el) { while (el.firstChild) el.removeChild(el.firstChild); return el; }
export function mount(el, ...children) { clear(el); append(el, children); return el; }
export function frag(...children) { const f = document.createDocumentFragment(); append(f, children); return f; }

// 安全 HTML: 转义后返回字符串（仅用于确认框等文本插值）
export function esc(s) {
  return String(s ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

export function fmtBytes(n) {
  if (n < 1024) return n + ' B';
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' KB';
  return (n / 1024 / 1024).toFixed(1) + ' MB';
}

export function fmtTime(s) {
  if (!s) return '—';
  const d = new Date(s);
  if (isNaN(d)) return s;
  const pad = n => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export function fmtClock(s) {
  if (!s) return '—';
  const d = new Date(s);
  if (isNaN(d)) return s;
  const pad = n => String(n).padStart(2, '0');
  return `${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export function remainText(expiresAt) {
  if (!expiresAt) return '长期';
  const t = new Date(expiresAt).getTime();
  if (isNaN(t)) return '—';
  const diff = t - Date.now();
  if (diff <= 0) return '已过期';
  const days = Math.floor(diff / 86400000);
  const hours = Math.floor((diff % 86400000) / 3600000);
  if (days > 0) return `剩余 ${days} 天`;
  if (hours > 0) return `剩余 ${hours} 小时`;
  return `剩余 ${Math.max(1, Math.floor(diff / 60000))} 分钟`;
}
