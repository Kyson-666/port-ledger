// 前端静态校验：语法解析 + 相对 import 路径可达性
// 用法：node scripts/check-frontend.mjs
import { readdirSync, statSync, readFileSync, existsSync, mkdtempSync, writeFileSync, rmSync } from 'node:fs';
import { join, dirname, resolve, extname, relative } from 'node:path';
import { spawnSync } from 'node:child_process';
import { tmpdir } from 'node:os';

const WEB = resolve('web');
if (!existsSync(WEB)) {
  console.error('未找到 web/ 目录');
  process.exit(1);
}

function walk(dir, out = []) {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) walk(p, out);
    else out.push(p);
  }
  return out;
}

const files = walk(WEB).filter(f => ['.js', '.mjs', '.css', '.html'].includes(extname(f)));
const jsFiles = files.filter(f => extname(f) === '.js' || extname(f) === '.mjs');

let errors = 0;
const importRe = /(?:^|\n)\s*(?:import\s+(?:[^'"\n]*?from\s*)?|export\s+[^'"\n]*?from\s*)['"]([^'"]+)['"]/g;
const dynRe = /import\(\s*['"]([^'"]+)['"]\s*\)/g;

const tmp = mkdtempSync(join(tmpdir(), 'pl-check-'));

for (const f of jsFiles) {
  const src = readFileSync(f, 'utf8');
  const rel = relative(WEB, f);

  // 1) 语法检查（复制为 .mjs 以便按 ES module 解析）
  const tmpFile = join(tmp, rel.replace(/[\\/]/g, '_') + '.mjs');
  writeFileSync(tmpFile, src);
  const res = spawnSync(process.execPath, ['--check', tmpFile], { encoding: 'utf8' });
  if (res.status !== 0) {
    console.error(`✗ 语法错误 ${rel}\n${res.stderr.trim()}`);
    errors++;
  }

  // 2) import 路径可达性
  const specs = new Set();
  for (const m of src.matchAll(importRe)) specs.add(m[1]);
  for (const m of src.matchAll(dynRe)) specs.add(m[1]);
  for (const spec of specs) {
    if (!spec.startsWith('.')) continue; // 仅校验相对路径
    const target = resolve(dirname(f), spec);
    if (!existsSync(target)) {
      console.error(`✗ import 目标不存在 ${rel} → ${spec}`);
      errors++;
    }
  }
}

// 3) index.html 引用的资源必须存在
const index = join(WEB, 'index.html');
if (existsSync(index)) {
  const html = readFileSync(index, 'utf8');
  for (const m of html.matchAll(/(?:src|href)="(\/[^"]+)"/g)) {
    const target = join(WEB, m[1].replace(/^\//, ''));
    if (!existsSync(target)) {
      console.error(`✗ index.html 引用不存在: ${m[1]}`);
      errors++;
    }
  }
}

// 4) 硬编码颜色审计（设计系统 §7 禁止清单：页面/组件不得写死 hex）
const HEX = /#[0-9A-Fa-f]{6}\b/g;
let hexHits = 0;
for (const f of jsFiles) {
  const rel = relative(WEB, f);
  const hits = readFileSync(f, 'utf8').match(HEX) || [];
  if (hits.length) {
    console.error(`✗ ${rel} 含硬编码颜色（应由 tokens.css 提供）: ${[...new Set(hits)].join(', ')}`);
    hexHits += hits.length;
    errors++;
  }
}

rmSync(tmp, { recursive: true, force: true });

console.log(`已检查 ${jsFiles.length} 个 JS 模块、${files.length} 个前端文件。`);
if (errors) {
  console.error(`校验失败：${errors} 项问题。`);
  process.exit(1);
}
console.log('前端静态校验通过：语法正确、import 可达、index.html 引用完整、无硬编码颜色。');
