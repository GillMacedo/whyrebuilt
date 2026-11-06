# whyrebuilt

Claude Code skill and Go CLI that explains why a Go build or `go test` run missed the build cache, by comparing the cache keys of two runs.

## Architecture
`plugin/scripts/whyrebuilt.go` holds all the logic in one standard-library file, so `go run` needs no setup. `cmdRecord` runs a command with `GODEBUG=gocachehash=1`, keeps only the stderr lines of compile (`build`), `vet` and `link` keys and the command's own messages, and writes a header and an end line with the exit code and line count. `ParseRecord` checks a record and groups the `HASH[name]` lines into actions. `Compare` pairs actions by name, diffs their inputs by `slot`, and `describe` turns each changed input into a cause. Inputs that only carry a dependency's output (`import`, `packagefile`, `packagemain`, `pkg`, `vetout`) become "because of them" links. `plugin/skills/explaining-go-build-cache-misses/SKILL.md` tells Claude how to use it. See `docs/architecture.md`.

## Commands
- `make test`: gofmt, vet, unit tests and integration tests in `tests/`
- `make lint`: plugin and marketplace validation
- `make demo`: compare the healthy pair and the moved-checkout pair of example records
- `make records`: record new example runs from `examples/inventory` (they differ on every run, so update the README after)

## Conventions
- Standard library only. Never use the network. `record` runs only the command it is given.
- Anything that can't be trusted exits 2 with nothing on stdout: empty, garbage, cut-off, edited or raw records, failed runs, compiler or vet errors in stderr, records without keys, the same record twice, runs with nothing in common. Add a test for each new input, on each side of `compare`, with and without `--fail`.
- The go command decides the inputs. Every cause in `describe` must match a line the go command writes, in `cmd/go/internal/work/exec.go`.
- Records in `examples/` are real output of `scripts/record-examples.sh` or of the commands in `examples/README.md`. Don't edit them by hand.

## Do and don't
- Do say that matching keys only help if B's cache holds A's results.
- Don't claim a time saving. The tool shows keys, not durations.
- Don't add explanations that guess. Unknown inputs are shown as "Other key input differs" with their raw values.

## Definition of done
`make test` and `make lint` pass, and `ROADMAP.md`, the README and docs are updated when relevant.
