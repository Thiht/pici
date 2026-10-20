import { defineConfig } from 'vitepress'

export default defineConfig({
  title: 'pici',
  description: 'A minimal, self-hosted CI system',
  lang: 'en-US',
  cleanUrls: true,
  lastUpdated: true,
  markdown: {
    lineNumbers: true,
    image: {
      lazyLoading: true,
    },
  },
  themeConfig: {
    nav: [
      { text: 'Guide', link: '/guide/getting-started' },
      { text: 'API', link: '/guide/api' },
    ],
    sidebar: [
      {
        text: 'Introduction',
        items: [
          { text: 'What is pici?', link: '/' },
          { text: 'Getting started', link: '/guide/getting-started' },
          { text: 'Authentication', link: '/guide/authentication' },
        ],
      },
      {
        text: 'Concepts',
        items: [
          { text: 'Projects', link: '/guide/projects' },
          { text: 'Workflows (.ci)', link: '/guide/workflows' },
          { text: 'ci.yml schema', link: '/guide/ci-schema' },
          { text: 'Variables & secrets', link: '/guide/variables' },
          { text: 'Webhooks & schedules', link: '/guide/webhooks' },
        ],
      },
      {
        text: 'Deployment',
        items: [
          { text: 'Docker', link: '/guide/docker' },
          { text: 'Fly.io', link: '/guide/fly' },
        ],
      },
      {
        text: 'Reference',
        items: [
          { text: 'API', link: '/guide/api' },
          { text: 'API reference (Swagger)', link: '/guide/api-reference' },
          { text: 'CLI', link: '/guide/cli' },
          { text: 'Configuration', link: '/guide/configuration' },
        ],
      },
    ],
    search: {
      provider: 'local',
    },
    editLink: {
      pattern: 'https://github.com/Thiht/pici/edit/main/docs/:path',
      text: 'Edit this page',
    },
    externalLinkIcon: true,
    footer: {
      message: 'A minimal, self-hosted CI system',
      copyright: '© 2026 pici',
    },
    notFound: {
      title: 'Page not found',
      quote: 'The page you are looking for does not exist.',
      linkLabel: 'go to the guide',
      linkText: 'Go to the guide',
      link: '/guide/getting-started',
    },
    socialLinks: [{ icon: 'github', link: 'https://github.com/Thiht/pici' }],
  },
})
