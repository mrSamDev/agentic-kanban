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
        const url = page.url;
        const isEnHome = url === 'https://mrsamdev.github.io/agentic-kanban/';
        const isZhHome = url === 'https://mrsamdev.github.io/agentic-kanban/zh/';
        const entry = {
          url,
          changefreq: 'weekly',
          priority: isEnHome || isZhHome ? 1.0 : 0.8,
        };
        // Add hreflang alternates for translated homepages
        if (isEnHome || isZhHome) {
          entry.links = [
            { url: 'https://mrsamdev.github.io/agentic-kanban/', rel: 'alternate', lang: 'en' },
            { url: 'https://mrsamdev.github.io/agentic-kanban/zh/', rel: 'alternate', lang: 'zh' },
            { url: 'https://mrsamdev.github.io/agentic-kanban/', rel: 'alternate', lang: 'x-default' },
          ];
        }
        return entry;
      },
    }),
  ],
});
