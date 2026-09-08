import { defineConfig, loadEnv } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), 'MENDER_');
  const target = env.MENDER_API_PROXY_TARGET || 'http://127.0.0.1:18080';
  return {
    plugins: [react(), tailwindcss()],
    server: {
      host: '127.0.0.1',
      port: 5173,
      strictPort: true,
      proxy: { '/healthz': target, '/readyz': target, '/api': target },
    },
    preview: { host: '127.0.0.1', port: 6173, strictPort: true },
  };
});
