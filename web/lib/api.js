// API 客户端：统一错误处理，响应体为 JSON。
async function req(url, opts) {
  const res = await fetch(url, opts);
  const text = await res.text();
  let data = null;
  if (text) { try { data = JSON.parse(text); } catch { data = text; } }
  if (!res.ok) {
    const msg = (data && data.error) ? data.error : `${res.status} ${res.statusText}`;
    const err = new Error(msg);
    err.status = res.status;
    throw err;
  }
  return data;
}

function qs(params) {
  const p = new URLSearchParams();
  for (const [k, v] of Object.entries(params || {})) {
    if (v === undefined || v === null || v === '') continue;
    if (Array.isArray(v)) { if (v.length) p.set(k, v.join(',')); continue; }
    p.set(k, String(v));
  }
  const s = p.toString();
  return s ? '?' + s : '';
}

const json = (method, body) => ({
  method,
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify(body ?? {}),
});

export const api = {
  health: () => req('/api/health'),
  scan: (body) => req('/api/scan', json('POST', body || {})),

  ports: (params) => req('/api/ports' + qs(params)),
  port: (port, protocol) => req(`/api/ports/${port}` + qs({ protocol })),
  putPort: (port, body) => req(`/api/ports/${port}`, json('PUT', body)),

  free: (params) => req('/api/free' + qs(params)),

  reservations: (params) => req('/api/reservations' + qs(params)),
  createReservation: (body) => req('/api/reservations', json('POST', body)),
  updateReservation: (body) => req('/api/reservations', json('PUT', body)),
  deleteReservation: (id) => req('/api/reservations' + qs({ id }), { method: 'DELETE' }),

  dict: (params) => req('/api/system-dict' + qs(params)),
  dictPost: (body) => req('/api/system-dict', json('POST', body)),
  dictImportPreview: (body) => req('/api/dict/import?preview=1', json('POST', body)),
  dictImport: (body) => req('/api/dict/import', json('POST', body)),
  dictExportURL: (download) => '/api/dict/export' + (download ? '?download=1' : ''),

  audit: (params) => req('/api/audit' + qs(params)),
  logs: (params) => req('/api/logs' + qs(params)),
  logsDownloadURL: () => '/api/logs?download=1',

  backupExportURL: () => '/api/backup/export',
  backupPreview: (body) => req('/api/backup/import?preview=1', json('POST', body)),
  backupImport: (body) => req('/api/backup/import', json('POST', body)),

  settings: () => req('/api/settings'),
  putSettings: (body) => req('/api/settings', json('PUT', body)),
  resetSettings: () => req('/api/settings', { method: 'DELETE' }),
};

export function downloadText(name, text, type = 'application/json') {
  const blob = new Blob([text], { type });
  const a = document.createElement('a');
  a.href = URL.createObjectURL(blob);
  a.download = name;
  a.click();
  setTimeout(() => URL.revokeObjectURL(a.href), 2000);
}

export async function readFileAsText(file) {
  return await file.text();
}
