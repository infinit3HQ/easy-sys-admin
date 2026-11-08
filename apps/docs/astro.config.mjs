import { defineConfig } from 'astro/config'
import starlight from '@astrojs/starlight'
import starlightLinksValidator from 'starlight-links-validator'

export default defineConfig({
  site: 'https://docs.easysysadmin.app',
  trailingSlash: 'never',
  build: { format: 'file' },
  integrations: [
    starlight({
      title: 'Easy Sys Admin',
      description:
        'Documentation for Easy Sys Admin: the hosted beta, the CLI, MCP, the API, and self-hosting.',
      logo: { src: './src/assets/logo.svg', alt: 'Easy Sys Admin' },
      customCss: ['./src/styles/custom.css'],
      favicon: '/favicon.svg',
      head: [
        {
          tag: 'meta',
          attrs: { name: 'theme-color', content: '#0b0d0c' },
        },
      ],
      social: [
        {
          icon: 'github',
          label: 'GitHub',
          href: 'https://github.com/infinit3HQ/easy-sys-admin',
        },
        {
          icon: 'email',
          label: 'Email',
          href: 'mailto:support@easysysadmin.app',
        },
      ],
      lastUpdated: false,
      plugins: [starlightLinksValidator()],
      sidebar: [
        {
          label: 'Start',
          items: [
            { label: 'Introduction', link: '/' },
            { label: 'Quickstart', link: '/quickstart' },
            { label: 'How it works', link: '/how-it-works' },
          ],
        },
        {
          label: 'Use it',
          items: [
            { label: 'Features', link: '/features' },
            { label: 'CLI', link: '/cli' },
            { label: 'MCP', link: '/mcp' },
            { label: 'API', link: '/api' },
          ],
        },
        {
          label: 'Run it',
          items: [
            { label: 'Self-hosting', link: '/self-hosting' },
            { label: 'Remove a server', link: '/remove-a-server' },
          ],
        },
        {
          label: 'Project',
          items: [
            { label: 'Pricing and plans', link: '/pricing' },
            { label: 'License', link: '/license' },
            { label: 'Troubleshooting', link: '/troubleshooting' },
            { label: 'Security', link: '/security' },
            { label: 'Changelog', link: '/changelog' },
          ],
        },
      ],
    }),
  ],
})
