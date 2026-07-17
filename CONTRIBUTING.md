# Contributing to RadioLedger

Thanks for wanting to contribute. Here's how we work.

## The Non-Negotiables

These aren't guidelines. They're requirements. PRs that don't meet them won't be merged.

1. **Every change has tests.** New feature? Tests. Bug fix? Regression test. No exceptions.
2. **Every change has documentation.** New API endpoint? Update the OpenAPI spec and the relevant doc page. New schema column? Add a `COMMENT ON`. New feature? Update the user guide.
3. **Docs are discoverable.** If you create a public guide, link it from the relevant docs index or navigation page.
4. **CI must be green.** All tests pass, linter is clean, coverage threshold met.

## Getting Started

### Prerequisites

- Go 1.22+ (`/opt/homebrew/bin/go` on macOS, check with `go version`)
- Docker and Docker Compose (for PostgreSQL + PostGIS test containers)
- Node.js 20+ (for web UI and Playwright tests)
- Make

### Clone and Build

```bash
git clone https://github.com/FtlC-ian/radioledger.git
cd radioledger

# Install JavaScript dependencies
pnpm install --frozen-lockfile

# Build the supported API, web, and documentation artifacts
make build

# Run supported unit tests and lint checks
make test
make lint

# For a local Docker stack, initialize secrets first, then start it.
cd docker
./init.sh
docker compose up -d

# Database migrations require a DATABASE_URL and goose.
cd ..
DATABASE_URL='postgres://…' make migrate-up
```

### Project Structure

```
radioledger/
├── api/              # Go API server
├── web/              # Web UI (SPA)
├── desktop/          # Tauri desktop client (Rust)
├── mobile/           # Mobile app
├── database/
│   ├── migrations/   # goose SQL migrations
│   └── seeds/        # Reference data
├── docker/           # Docker Compose files
├── pkg/              # Shared Go packages (ADIF parser, etc.)
├── testdata/         # Test fixtures
├── docs/             # Public documentation and contributor guides
├── CONTRIBUTING.md   # This file
└── Makefile          # Build/test/lint targets
```

## Developer Guides

Detailed guides live in `docs/contributing/`:

- **[Development Setup](docs/contributing/development-setup.md)** — Full local environment setup
- **[Architecture Overview](docs/contributing/architecture-overview.md)** — How the system fits together
- **[Database Guide](docs/contributing/database-guide.md)** — Schema conventions, writing migrations, using sqlc
- **[Testing Guide](docs/contributing/testing-guide.md)** — How to write and run each type of test
- **[Code Style](docs/contributing/code-style.md)** — Go, TypeScript, and SQL conventions
- **[Pull Requests](docs/contributing/pull-requests.md)** — PR process, what reviewers look for
- **[ADIF Reference](docs/contributing/adif-reference.md)** — ADIF format quick reference for devs

## How to Contribute

### Reporting Bugs

Open an issue with:
- What you expected to happen
- What actually happened
- Steps to reproduce
- Your environment (OS, browser, RadioLedger version)
- ADIF file if relevant (strip any personal data first)

### Suggesting Features

Open an issue with the `feature` label. Describe:
- The problem you're solving (not just the solution you want)
- Who benefits (casual operator, contester, POTA activator, self-hoster, etc.)
- Any prior art in other logging software

### Submitting Code

1. **Check for an existing issue** — if there isn't one, create it first
2. **Fork and branch** — branch from `main`, use descriptive branch names (`feat/adif-parser`, `fix/lotw-sync-timeout`)
3. **Write the code** — follow the conventions in the code-style guide
4. **Write the tests** — see the testing guide for what's expected per change type
5. **Update the docs** — if you changed behavior, update the relevant docs
6. **Open a PR** — reference the issue, describe what changed and why
7. **Address review feedback** — we review promptly, please respond promptly

### PR Checklist

Every PR should be able to answer yes to all of these:

- [ ] Tests added/updated for the change
- [ ] All CI checks pass (tests, lint, coverage)
- [ ] Documentation updated (code comments, doc pages, OpenAPI spec as applicable)
- [ ] New public doc files are linked from the relevant docs index or navigation page
- [ ] Schema changes include `COMMENT ON` statements
- [ ] Schema changes include a goose migration file
- [ ] No secrets, credentials, or personal data in the commit
- [ ] Commit messages follow conventional commits format

## What We're Looking For

Good first issues are labeled `good-first-issue`. These are scoped, well-documented tasks where the expected approach is clear.

Areas where we especially welcome contributions:
- **ADIF compatibility** — test files from logging programs we haven't tested with
- **Translations** — i18n for the docs and web UI
- **Sync adapters** — new external service integrations
- **Mobile features** — POTA/SOTA workflow improvements
- **Documentation** — filling in stub pages, improving explanations, adding screenshots
- **Bug reports** — especially from real-world usage with different logging programs

## Code of Conduct

Be decent. We're ham radio operators — we already know how to share a band. Treat contributors the way you'd treat someone helping you put up an antenna.

## License

By contributing, you agree that your contributions will be licensed under the AGPLv3 license.
