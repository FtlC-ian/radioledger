# RadioLedger Site (Astro + Starlight)

Marketing landing page and docs site for RadioLedger.

## Local development

```bash
corepack enable
corepack prepare pnpm@11.3.0 --activate
pnpm install
pnpm run dev
```

## Build

```bash
pnpm run build
```

Static output is generated in `dist/`.

## Structure

- `src/pages/index.astro` — landing page
- `src/content/docs/` — Starlight documentation pages
- `src/styles/site.css` — brand styling (dark-first)
- `public/` — favicon, OG image, robots.txt, llms.txt
