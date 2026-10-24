import { fileURLToPath } from 'node:url';
import { defineConfig, loadEnv } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

/**
 * One Go backend serves every API behind this app, on one origin:
 *
 *   /api/v1/*         discovery: sign-in, inventory, access graph, credential
 *                     exposure, integrations
 *   /api/v1/ispm/*    ISPM posture
 *   /api/v1/genome/*  NHI Genome
 *
 * so the dev server proxies /api to it (API_UPSTREAM). Production nginx does
 * the same (nginx/nginx.conf.template).
 */
const ROOT = fileURLToPath(new URL('.', import.meta.url));

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, ROOT, '');
  const upstream = (name, fallback) => (env[name] || fallback).trim();

  return {
    envDir: ROOT,
    plugins: [react(), tailwindcss()],
    server: {
      port: 3000,
      proxy: {
        '/api': { target: upstream('API_UPSTREAM', 'http://localhost:8080'), changeOrigin: true },
        // The discovery scanner's own scan API (start / status), kept same-origin.
        '/scan-api': {
          target: upstream('DISCOVERY_UPSTREAM', 'http://localhost:8081'),
          changeOrigin: true,
          rewrite: (path) => path.replace(/^\/scan-api/, ''),
        },
      },
    },
    build: { outDir: 'dist', sourcemap: false },
  };
});
