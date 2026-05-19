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
            { label: 'Getting Started', link: '/getting-started/' },
            { label: 'API', link: '/api/' },
          ],
        },
      ],
    }),
  ],
});
