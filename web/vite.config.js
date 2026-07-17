import { defineConfig } from 'vite'

export default defineConfig({
  // Relative asset paths so files work when embedded under Go's root.
  base: './',
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
})
