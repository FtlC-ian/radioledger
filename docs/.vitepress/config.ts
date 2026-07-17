import { defineConfig } from 'vitepress'

export default defineConfig({
  // Don't fail the build on dead links (many docs cross-reference internal files)
  ignoreDeadLinks: true,

  // Exclude internal agent docs (ALLCAPS), research notes, and private files
  srcExclude: [
    '[A-Z][A-Z]*.md',       // Root-level ALLCAPS internal docs (ARCHITECTURE.md, etc.)
    'api-research/**',
    'competitive-analysis/**',
    'legal/**',
    'llms.txt',
  ],

  title: 'RadioLedger Docs',
  description: 'Documentation for RadioLedger — the open-source amateur radio logbook',
  base: '/docs/',

  themeConfig: {
    logo: '/logo.png',

    nav: [
      { text: 'Home', link: '/' },
      { text: 'Getting Started', link: '/getting-started/' },
      { text: 'User Guide', link: '/user-guide/' },
      { text: 'Self-Hosting', link: '/self-hosting/' },
      { text: '← Back to App', link: '/', target: '_self' },
    ],

    sidebar: [
      {
        text: 'Getting Started',
        collapsed: false,
        items: [
          { text: 'Overview', link: '/getting-started/' },
          { text: 'Installation', link: '/getting-started/installation' },
          { text: 'Your First QSO', link: '/getting-started/first-qso' },
          { text: 'Import Existing Log', link: '/getting-started/import-existing-log' },
          { text: 'Connect Services', link: '/getting-started/connect-services' },
        ],
      },
      {
        text: 'User Guide',
        collapsed: false,
        items: [
          { text: 'Overview', link: '/user-guide/' },
          { text: 'Logging QSOs', link: '/user-guide/logging-qsos' },
          { text: 'Logbooks', link: '/user-guide/logbooks' },
          { text: 'Search & Filter', link: '/user-guide/search-and-filter' },
          { text: 'Import & Export', link: '/user-guide/import-export' },
          { text: 'Awards Tracking', link: '/user-guide/awards-tracking' },
          { text: 'Statistics', link: '/user-guide/statistics' },
          { text: 'QSL Management', link: '/user-guide/qsl-management' },
          { text: 'Settings', link: '/user-guide/settings' },
        ],
      },
      {
        text: 'Sync Services',
        collapsed: true,
        items: [
          { text: 'Overview', link: '/sync/' },
          { text: 'LoTW', link: '/sync/lotw' },
          { text: 'QRZ', link: '/sync/qrz' },
          { text: 'eQSL', link: '/sync/eqsl' },
          { text: 'ClubLog', link: '/sync/clublog' },
          { text: 'POTA', link: '/sync/pota' },
          { text: 'SOTA', link: '/sync/sota' },
        ],
      },
      {
        text: 'Desktop Client',
        collapsed: true,
        items: [
          { text: 'Overview', link: '/desktop/' },
          { text: 'Installation', link: '/desktop/installation' },
          { text: 'WSJT-X Setup', link: '/desktop/wsjtx-setup' },
          { text: 'JS8Call Setup', link: '/desktop/js8call-setup' },
          { text: 'N1MM+ Setup', link: '/desktop/n1mm-setup' },
          { text: 'Rig Control', link: '/desktop/rig-control' },
          { text: 'LoTW Certificates', link: '/desktop/lotw-certificates' },
          { text: 'Troubleshooting', link: '/desktop/troubleshooting' },
        ],
      },
      {
        text: 'Mobile App',
        collapsed: true,
        items: [
          { text: 'Overview', link: '/mobile/' },
          { text: 'Installation', link: '/mobile/installation' },
          { text: 'POTA Activation', link: '/mobile/pota-activation' },
          { text: 'SOTA Activation', link: '/mobile/sota-activation' },
          { text: 'Offline Logging', link: '/mobile/offline-logging' },
          { text: 'Sync', link: '/mobile/sync' },
        ],
      },
      {
        text: 'Contest Logging',
        collapsed: true,
        items: [
          { text: 'Overview', link: '/contest/' },
          { text: 'Setup', link: '/contest/setup' },
          { text: 'Multi-Op', link: '/contest/multi-op' },
          { text: 'Cabrillo Export', link: '/contest/cabrillo-export' },
          { text: 'N1MM+ Integration', link: '/contest/n1mm-integration' },
        ],
      },
      {
        text: 'Self-Hosting',
        collapsed: false,
        items: [
          { text: 'Overview', link: '/self-hosting/' },
          { text: 'Requirements', link: '/self-hosting/requirements' },
          { text: 'Docker Setup', link: '/self-hosting/docker-setup' },
          { text: 'Configuration', link: '/self-hosting/configuration' },
          { text: 'Updating', link: '/self-hosting/updating' },
          { text: 'Backup & Restore', link: '/self-hosting/backup-restore' },
          { text: 'Reverse Proxy', link: '/self-hosting/reverse-proxy' },
          { text: 'Security', link: '/self-hosting/security' },
        ],
      },
      {
        text: 'API Reference',
        collapsed: true,
        items: [
          { text: 'Overview', link: '/api/' },
          { text: 'Authentication', link: '/api/authentication' },
          { text: 'Rate Limits', link: '/api/rate-limits' },
          { text: 'Errors', link: '/api/errors' },
          { text: 'Pagination', link: '/api/pagination' },
          { text: 'Webhooks', link: '/api/webhooks' },
          {
            text: 'Endpoints',
            items: [
              { text: 'QSOs', link: '/api/endpoints/qsos' },
              { text: 'Logbooks', link: '/api/endpoints/logbooks' },
              { text: 'Import & Export', link: '/api/endpoints/import-export' },
              { text: 'Sync', link: '/api/endpoints/sync' },
              { text: 'Awards', link: '/api/endpoints/awards' },
              { text: 'Activations', link: '/api/endpoints/activations' },
              { text: 'Search', link: '/api/endpoints/search' },
              { text: 'Users', link: '/api/endpoints/users' },
            ],
          },
        ],
      },
      {
        text: 'Contributing',
        collapsed: true,
        items: [
          { text: 'Overview', link: '/contributing/' },
          { text: 'Development Setup', link: '/contributing/development-setup' },
          { text: 'Architecture Overview', link: '/contributing/architecture-overview' },
          { text: 'Testing Guide', link: '/contributing/testing-guide' },
          { text: 'Code Style', link: '/contributing/code-style' },
          { text: 'Pull Requests', link: '/contributing/pull-requests' },
          { text: 'ADIF Reference', link: '/contributing/adif-reference' },
        ],
      },
      {
        text: 'Reference',
        collapsed: true,
        items: [
          { text: 'Overview', link: '/reference/' },
          { text: 'ADIF Field Mapping', link: '/reference/adif-field-mapping' },
          { text: 'Bands & Modes', link: '/reference/bands-and-modes' },
          { text: 'DXCC Entities', link: '/reference/dxcc-entities' },
          { text: 'Callsign Parsing', link: '/reference/callsign-parsing' },
          { text: 'Maidenhead Grids', link: '/reference/maidenhead-grids' },
          { text: 'Glossary', link: '/reference/glossary' },
        ],
      },
      {
        text: 'More',
        collapsed: true,
        items: [
          { text: 'Security', link: '/security' },
          { text: 'FAQ', link: '/faq' },
          { text: 'Changelog', link: '/changelog/' },
        ],
      },
    ],

    socialLinks: [
      { icon: 'github', link: 'https://github.com/FtlC-ian/radioledger' },
    ],

    footer: {
      message: 'Open-source amateur radio logbook',
      copyright: 'RadioLedger — <a href="https://radioledger.app">radioledger.app</a>',
    },

    search: {
      provider: 'local',
    },

    editLink: {
      pattern: 'https://github.com/FtlC-ian/radioledger/edit/main/docs/:path',
      text: 'Edit this page on GitHub',
    },
  },

  // Custom CSS for RadioLedger branding
  head: [
    ['style', {}, `
      :root {
        --vp-c-brand-1: #E89E3B;
        --vp-c-brand-2: #d08a28;
        --vp-c-brand-3: #c07820;
        --vp-c-brand-soft: rgba(232, 158, 59, 0.14);
        --vp-button-brand-bg: #E89E3B;
        --vp-button-brand-hover-bg: #d08a28;
      }
    `],
  ],
})
