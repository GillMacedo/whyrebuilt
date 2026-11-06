# Changelog

All notable changes are documented here, in [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) format. This project uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.1.0] - 2026-11-06

### Added
- `record` command: runs a go command with `GODEBUG=gocachehash=1` and saves the inputs of every compile, vet and link cache key, with the exit code, a line count and the number of `GOCACHE` entries before and after. Writes gzip when the file name ends in `.gz`
- `compare` command: compares two records and groups the inputs that changed into causes, such as the checkout directory without `-trimpath`, the Go version, compiler and linker flags, cgo settings, build stamps, `GOROOT` and source files, and lists the steps that changed only because a dependency did
- Notes when B's `GOCACHE` was empty or unreadable, or the runs used different cache paths or commands
- Refuses empty, garbage, cut-off, edited and raw records, failed runs, runs whose stderr shows a compiler or vet error, records with no cache keys, the same record twice, and runs with no step in common (exit code 2)
- `--fail`, `--json` and `--limit` options
- Claude Code plugin with the `explaining-go-build-cache-misses` skill
