# Contributing to RadioLedger

> Welcome! RadioLedger is open source (AGPLv3) and contributions are encouraged.

## Ways to Contribute

| Type | Where to start |
|------|---------------|
| Bug reports | [GitHub Issues](https://github.com/FtlC-ian/radioledger/issues) |
| Feature requests | GitHub Issues with "enhancement" label |
| Code contributions | [Development Setup](development-setup.md) + [Pull Requests](pull-requests.md) |
| Documentation | Edit any `.md` file in `docs/` — typo fixes merge without review |
| Testing | Run the test suite, report flaky tests |
| Translations | Translate docs to German, Japanese, Portuguese, Spanish |

## Before You Start

Read [AGENTS.md](../../AGENTS.md) — the project rules are mandatory for all contributors. Key points:

- **Documentation is mandatory**: Every change ships with docs
- **Tests are mandatory**: Every feature, every bug fix
- **Schema is sacred**: Read the full schema before touching the database
- **Security first**: RLS, parameterized queries, no plaintext credentials

## In This Section

| Guide | What it covers |
|-------|---------------|
| [development-setup.md](development-setup.md) | Clone, build, and run locally |
| [architecture-overview.md](architecture-overview.md) | High-level system design |
| [database-guide.md](database-guide.md) | Schema conventions, migrations, sqlc |
| [testing-guide.md](testing-guide.md) | How to write and run tests |
| [code-style.md](code-style.md) | Go, TypeScript, SQL coding conventions |
| [pull-requests.md](pull-requests.md) | PR process and review expectations |
| [adif-reference.md](adif-reference.md) | ADIF format quick reference |

## Community Standards

- Be respectful — ham radio is a welcoming hobby; so is this project
- English is the working language for code and issues (docs translations welcome in all languages)
- No gatekeeping — new hams and new developers are both welcome

## License

RadioLedger is [AGPLv3](https://www.gnu.org/licenses/agpl-3.0.html). Contributions must be compatible with AGPLv3. By submitting a PR, you agree your contribution is licensed under AGPLv3.

## Related

- [AGENTS.md](../../AGENTS.md) — mandatory reading for all contributors
- [Architecture](../ARCHITECTURE.md)
- [Schema](../SCHEMA.md)
- [Testing Architecture](../TESTING_ARCHITECTURE.md)
