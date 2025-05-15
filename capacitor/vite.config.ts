import { defineConfig } from 'vite';
import { viteStaticCopy } from 'vite-plugin-static-copy';

export default defineConfig({
  root: './src', // Your project root is src/, so paths for viteStaticCopy will be relative to this
  build: {
    outDir: '../dist', // Output directory is dist/ relative to the vite.config.ts location
    minify: false,
    emptyOutDir: true,
  },
  plugins: [
    viteStaticCopy({
      targets: [
        {
          src: 'how-it-works.html', // Correctly relative to root ('src/')
          dest: '.' // Copies to dist/how-it-works.html
        },
        {
          src: 'sponsor.html',
          dest: '.'
        },
        {
          src: 'styles.css',
          dest: '.'
        },
        {
          src: 'manifest.json',
          dest: '.'
        },
        {
          src: 'robots.txt',
          dest: '.'
        },
        {
          src: 'sitemap.xml',
          dest: '.'
        },
        {
          src: 'assets', // Copies the entire assets folder from src/assets
          dest: '.'    // to dist/assets
        },
        {
          src: 'css',  // Copies the entire css folder from src/css
          dest: '.'   // to dist/css
        }
      ]
    })
  ],
});
