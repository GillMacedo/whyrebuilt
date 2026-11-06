# 0001: One standard-library Go file in a skill

**Decision:** the tool is a single Go file with no dependencies, run with `go run`, bundled in a Claude Code skill.

**Why:** anyone who wants to know why Go rebuilds has Go, so `go run` is the whole install, locally and in a CI step. An agent can already run a script and read its output, so an MCP server would add a binary and an npm package without giving the agent anything new.

**Alternatives:** an MCP server with the official Go SDK, like `stuckgo`. It would make sense if agents needed this without a shell, which isn't the case for a tool that wraps a go command.
