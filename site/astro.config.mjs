// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

// https://astro.build/config
export default defineConfig({
	site: 'https://docs.radioledger.app',
	integrations: [
		starlight({
			title: 'RadioLedger Docs',
			description:
				'Documentation for RadioLedger — the open-source ledger for radio amateurs.',
			tagline: 'Your log. Your data. Your server if you want it.',
			favicon: '/favicon.svg',
			social: [
				{
					icon: 'github',
					label: 'GitHub',
					href: 'https://github.com/FtlC-ian/radioledger',
				},
			],
			customCss: ['/src/styles/site.css'],
			components: {
				ThemeProvider: './src/components/ThemeProvider.astro',
			},
			editLink: {
				baseUrl:
					'https://github.com/FtlC-ian/radioledger/edit/main/site/src/content/docs/',
			},
			lastUpdated: true,
			credits: false,
			head: [
				{ tag: 'meta', attrs: { property: 'og:type', content: 'website' } },
				{
					tag: 'meta',
					attrs: {
						property: 'og:image',
						content: 'https://docs.radioledger.app/og-radioledger.svg',
					},
				},
				{ tag: 'meta', attrs: { name: 'twitter:card', content: 'summary_large_image' } },
				{
					tag: 'meta',
					attrs: {
						name: 'twitter:image',
						content: 'https://docs.radioledger.app/og-radioledger.svg',
					},
				},
			],
			sidebar: [
				{
					label: 'Getting Started',
					items: [{ slug: 'docs' }, { slug: 'getting-started' }, { slug: 'self-hosting' }],
				},
				{
					label: 'Core Guides',
					items: [
						{ slug: 'adif-import-export' },
						{ slug: 'desktop-client' },
						{ slug: 'pota-sota' },
					],
				},
				{
					label: 'API',
					items: [{ slug: 'api-reference' }],
				},
				{
					label: 'Roadmap',
					items: [
						{
							slug: 'federation',
							badge: { text: 'Coming soon', variant: 'note' },
						},
					],
				},
			],
		}),
	],
});
