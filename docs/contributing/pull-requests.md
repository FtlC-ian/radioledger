# Pull Request Process

> How to submit and review pull requests for RadioLedger.

## Before You Open a PR

1. **Read AGENTS.md** — the project rules are mandatory
2. **For new features**: open an issue first to discuss the approach
3. **For bug fixes**: reference the issue in your PR
4. **For schema changes**: read the full SCHEMA.md and Architecture doc first
5. **Run tests**: `make test` must pass
6. **Run lint**: `make lint` must pass
7. **Update docs**: if your change affects user-facing behavior, update the relevant docs

## Branch Naming

```
feat/short-description
fix/issue-123-short-description
docs/update-api-auth-guide
refactor/extract-callsign-package
```

Branch off `main`. Never commit directly to `main`.

## PR Requirements

Your PR must include:

### Code Changes
- Implementation
- Tests (unit + integration as appropriate)
- RLS isolation test if you added a new tenant-scoped table
- ADIF corpus test if you changed the ADIF parser

### Documentation
- Updated user-facing docs if behavior changed
- Updated `docs/SCHEMA.md` if schema changed
- Updated `docs/INDEX.md` if you created new doc files
- API docs (OpenAPI spec) if endpoint changed
- Database `COMMENT ON` for new/changed columns, tables, types

### PR Description Template

```markdown
## What this does
Brief description of the change.

## Why
The motivation or issue reference (#123).

## How to test
Steps to verify the change works.

## Checklist
- [ ] Tests added/updated
- [ ] Documentation updated
- [ ] SCHEMA.md updated (if schema changed)
- [ ] INDEX.md updated (if new docs created)
- [ ] COMMENT ON added (if schema changed)
- [ ] RLS test added (if new tenant table)
```

## Review Process

| Change type | Reviewers needed |
|-------------|-----------------|
| Typo/doc fix | 0 (auto-merge) |
| Bug fix | 1 reviewer |
| New feature | 1 reviewer + author approval |
| Schema change | 2 reviewers (one must be schema-familiar) |
| Security change | 2 reviewers (one security-focused) |
| Breaking API change | 2 reviewers + issue discussion |

## Merging

- **Squash merge** to main for clean history
- Commit message is the PR title (must follow conventional commits)
- CI must be green (all tests pass, lint clean)
- Don't force-push to `main`

## After Merging

- Delete your branch
- Close the related issue (if applicable)
- If a schema migration was included: verify it runs on staging

## Related

- [Development Setup](development-setup.md)
- [Testing Guide](testing-guide.md)
- [Code Style](code-style.md)
