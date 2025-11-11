# Claude Code Guidelines

## Commit Message Format

When creating commits, use the following format:

**Format:** One-line, lowercase, conventional commits without Claude attribution

**Examples:**
```
add automatic telemetry collection from ebu
fix context key type mismatch in telemetry collector
update readme with telemetry documentation
refactor http client to be shared between store and telemetry
test: achieve 100% coverage for telemetry collector
```

**Rules:**
- Use conventional commit types: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`, etc.
- All lowercase (no capitalization)
- Single line summary (no body unless absolutely necessary)
- No emoji
- No "Generated with Claude Code" attribution
- No "Co-Authored-By: Claude" attribution
- Be concise and descriptive

**DO NOT use:**
- ❌ Multi-line commit messages with bodies
- ❌ Claude attribution footers
- ❌ Emoji in commit messages
- ❌ Capitalized first letter
- ❌ Period at the end of the summary

**DO use:**
- ✅ Conventional commit prefixes when applicable
- ✅ Imperative mood ("add" not "added")
- ✅ Clear, concise description of changes
