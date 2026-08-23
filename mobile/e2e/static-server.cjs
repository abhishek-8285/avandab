/* eslint-env node */
// Zero-dependency static file server for the Playwright webServer block.
// Serves an expo web export directory on E2E_PORT (default 8099).
const http = require('node:http');
const fs = require('node:fs');
const path = require('node:path');

const PORT = Number(process.env.E2E_PORT || 8099);
const ROOT = process.env.E2E_ROOT || path.join(__dirname, '..', 'dist-web');

const MIME = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'application/javascript',
  '.css': 'text/css',
  '.json': 'application/json',
  '.png': 'image/png',
  '.jpg': 'image/jpeg',
  '.svg': 'image/svg+xml',
  '.ico': 'image/x-icon',
  '.map': 'application/json',
  '.hbc': 'application/octet-stream',
  '.abc': 'application/octet-stream',
  '.wasm': 'application/wasm',
};

const server = http.createServer((req, res) => {
  if (!fs.existsSync(path.join(ROOT, 'index.html'))) {
    // No export yet — keep listening so the Playwright health check passes;
    // the spec itself skips when the dist bundle is absent.
    res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' });
    res.end('<!doctype html><title>avandab e2e</title><p>dist-web export missing</p>');
    return;
  }
  try {
    const urlPath = decodeURIComponent(new URL(req.url, 'http://localhost').pathname);
    let filePath = path.normalize(path.join(ROOT, urlPath));
    if (!filePath.startsWith(ROOT)) {
      res.writeHead(403).end('forbidden');
      return;
    }
    if (!fs.existsSync(filePath) || fs.statSync(filePath).isDirectory()) {
      // SPA-ish fallback: serve index.html for unknown / directory paths
      filePath = path.join(ROOT, 'index.html');
    }
    const ext = path.extname(filePath).toLowerCase();
    res.writeHead(200, { 'Content-Type': MIME[ext] || 'application/octet-stream' });
    fs.createReadStream(filePath).pipe(res);
  } catch (_err) {
    res.writeHead(500).end('internal error');
  }
});

server.listen(PORT, () => {
  console.log(`e2e static server: ${ROOT} -> http://localhost:${PORT}`);
});
