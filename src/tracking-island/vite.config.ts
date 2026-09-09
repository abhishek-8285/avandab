import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import path from 'path';

// Single IIFE bundle + stylesheet, written straight into the Go static tree
// so the binary serves them via embed.FS with zero Node runtime in prod.
export default defineConfig({
  plugins: [react()],
  // React's entry checks process.env.NODE_ENV — no Node runtime in the
  // browser, so bake the constant in at compile time.
  define: { 'process.env.NODE_ENV': '"production"' },
  build: {
    outDir: path.resolve(__dirname, '../../internal/static/tracking-island'),
    emptyOutDir: true,
    cssCodeSplit: false,
    lib: {
      entry: path.resolve(__dirname, 'src/index.tsx'),
      name: 'TrackingIsland',
      formats: ['iife'],
      fileName: () => 'tracking.bundle.js',
    },
    rollupOptions: {
      output: {
        assetFileNames: (assetInfo) => {
          if (assetInfo.name?.endsWith('.css')) return 'tracking.bundle.css';
          return 'assets/[name]-[hash][extname]';
        },
      },
    },
  },
});
