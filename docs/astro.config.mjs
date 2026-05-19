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
            { label: 'Overview', link: '/godantic/' },
            { label: 'Getting Started', link: '/godantic/getting-started/' },
            { label: 'Architecture', link: '/godantic/architecture/' },
          ],
        },
        {
          label: 'Guides',
          items: [
            { label: 'Models', link: '/godantic/models/' },
            { label: 'Sessions', link: '/godantic/sessions/' },
            { label: 'Tools', link: '/godantic/tools/' },
            { label: 'Storage', link: '/godantic/storage/' },
            { label: 'Examples', link: '/godantic/examples/' },
            { label: 'Production', link: '/godantic/production/' },
          ],
        },
        {
          label: 'Reference',
          items: [
            { label: 'API', link: '/godantic/api/' },
            { label: 'Versioning', link: '/godantic/versioning/' },
          ],
        },
      ],
    }),
  ],
});
