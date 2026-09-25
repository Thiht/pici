import { defineConfig } from 'vitepress'

export default defineConfig({
  title: 'pici',
  description: 'A minimal, self-hosted CI system',
  lang: 'en-US',
  cleanUrls: true,
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
        ],
      },
      {
        text: 'Concepts',
        items: [
          { text: 'Projects', link: '/guide/projects' },
          { text: 'Workflows (.ci)', link: '/guide/workflows' },
          { text: 'Variables & secrets', link: '/guide/variables' },
          { text: 'Webhooks & schedules', link: '/guide/webhooks' },
        ],
      },
      {
        text: 'Reference',
        items: [
          { text: 'API', link: '/guide/api' },
          { text: 'CLI', link: '/guide/cli' },
          { text: 'Configuration', link: '/guide/configuration' },
          { text: 'Docker', link: '/guide/docker' },
        ],
      },
    ],
    socialLinks: [
      { icon: 'github', link: 'https://github.com/Thiht/pici' },
    ],
  },
})
