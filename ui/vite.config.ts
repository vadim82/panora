import { readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import { searchForWorkspaceRoot } from 'vite';

const iconMasters = fileURLToPath(
  new URL('../assets/branding/icons/2026-11-02/masters/', import.meta.url),
);

export default defineConfig(({ command }) => ({
  root: 'frontend',
  base: './',
  cacheDir: '../node_modules/.vite',
  plugins: [
    react(),
    {
      name: 'local-development-csp',
      apply: 'serve',
      configureServer(server) {
        server.middlewares.use(async (request, response, next) => {
          const path = request.url?.split('?')[0];
          if (request.method !== 'GET' || (path !== '/' && path !== '/index.html')) return next();
          try {
            const template = await readFile(
              new URL('./frontend/index.html', import.meta.url),
              'utf8',
            );
            const html = await server.transformIndexHtml(request.url!, template);
            const nonce = crypto.randomUUID();
            response.setHeader('Content-Type', 'text/html');
            response.end(html.replaceAll('__PANORA_DEV_NONCE__', nonce));
          } catch (error) {
            next(error);
          }
        });
      },
      transformIndexHtml(html) {
        // Vite injects scripts/styles after plugin transforms. The local HTML
        // middleware replaces their shared nonce placeholder after all transforms.
        return html
          .replace("script-src 'self'", "script-src 'self' 'nonce-__PANORA_DEV_NONCE__'")
          .replace("style-src 'self'", "style-src 'self' 'nonce-__PANORA_DEV_NONCE__'")
          .replace("connect-src 'self'", "connect-src 'self' ws://127.0.0.1:5173");
      },
    },
  ],
  html: command === 'serve' ? { cspNonce: '__PANORA_DEV_NONCE__' } : undefined,
  build: { outDir: 'dist', emptyOutDir: true, target: 'safari16' },
  server: {
    host: '127.0.0.1',
    port: 5173,
    strictPort: true,
    fs: {
      allow: [searchForWorkspaceRoot(fileURLToPath(new URL('.', import.meta.url))), iconMasters],
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    include: ['src/**/*.test.{ts,tsx}'],
    restoreMocks: true,
    clearMocks: true,
  },
}));
