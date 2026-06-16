// @ts-check
import { defineConfig } from 'astro/config';

// https://astro.build/config
export default defineConfig({
  site: 'https://mrsamdev.github.io',
  base: '/agentic-kanban',
  outDir: '_site',
  publicDir: 'public',
  build: {
    format: 'file',
  },
});
