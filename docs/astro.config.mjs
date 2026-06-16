// @ts-check
import { defineConfig } from 'astro/config';
import sitemap from '@astrojs/sitemap';

// https://astro.build/config
export default defineConfig({
  site: 'https://mrsamdev.github.io',
  base: '/agentic-kanban',
  outDir: '_site',
  publicDir: 'public',
  build: {
    format: 'directory',
  },
  integrations: [
    sitemap({
      serialize: (page) => {
        // Only include pages under /agentic-kanban/
        if (!page.url.startsWith('https://mrsamdev.github.io/agentic-kanban/')) {
          return undefined;
        }
        return {
          url: page.url,
          changefreq: 'weekly',
          priority: page.url === 'https://mrsamdev.github.io/agentic-kanban/' ? 1.0 : 0.8,
        };
      },
    }),
  ],
});
