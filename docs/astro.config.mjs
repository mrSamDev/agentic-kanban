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
          links: isEnHome || isZhHome
            ? [
                { url: 'https://mrsamdev.github.io/agentic-kanban/', lang: 'en' },
                { url: 'https://mrsamdev.github.io/agentic-kanban/zh/', lang: 'zh' },
                { url: 'https://mrsamdev.github.io/agentic-kanban/', lang: 'x-default' },
              ]
            : [],
        };
        return /** @type {import('@astrojs/sitemap').SitemapItem} */ (entry);
      },
    }),
  ],
});
