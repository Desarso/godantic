import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

export default defineConfig({
  site: 'https://desarso.github.io',
  base: '/godantic',
  integrations: [
    starlight({
      title: 'godantic',
      description: 'Go framework for LLM agents, tools, sessions, and chat history.',
      customCss: ['./src/styles/custom.css'],
      social: [
        { icon: 'github', label: 'GitHub', href: 'https://github.com/Desarso/godantic' },
      ],
      sidebar: [
        {
          label: 'Start Here',
          items: [
            { label: 'Overview', link: '/' },
            { label: 'Getting Started', slug: 'getting-started' },
            { label: 'Architecture', slug: 'architecture' },
          ],
        },
        {
          label: 'Guides',
          items: [
            { label: 'Models', slug: 'models' },
            { label: 'Sessions', slug: 'sessions' },
            { label: 'Tools', slug: 'tools' },
            { label: 'Storage', slug: 'storage' },
            { label: 'Examples', slug: 'examples' },
            { label: 'Production', slug: 'production' },
          ],
        },
        {
          label: 'Reference',
          items: [
            { label: 'API', slug: 'api' },
            { label: 'Versioning', slug: 'versioning' },
          ],
        },
      ],
    }),
  ],
});
