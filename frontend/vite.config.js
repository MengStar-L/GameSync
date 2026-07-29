import { defineConfig } from 'vite';

export default defineConfig({
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
  server: {
    host: '127.0.0.1',
    // PORT 由预览工具注入；未注入时保持 wails dev 期望的固定端口
    port: Number(process.env.PORT) || 34116,
    strictPort: !process.env.PORT,
  },
});
