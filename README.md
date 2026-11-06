# whyrebuilt: find out why Go rebuilds everything in CI even with the build cache restored

[![CI](https://github.com/GillMacedo/whyrebuilt/actions/workflows/ci.yml/badge.svg)](https://github.com/GillMacedo/whyrebuilt/actions/workflows/ci.yml) [![MIT license](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

**A Claude Code skill and Go CLI for Go teams whose CI restores `GOCACHE` (with setup-go, actions/cache or a shared runner) and still compiles every package. It records a `go build` or `go test` run with the go command's own cache key debug output, compares two runs, and names the inputs that changed: the checkout path without `-trimpath`, the Go version, `-race` or `-gcflags`, `-ldflags`, cgo settings, `GOROOT`, build stamps or a source file. It separates those root causes from the packages that rebuilt only because a dependency did.**

The go command looks up every compile, vet and link step in its build cache by a key hashed from that step's inputs. If one input differs, the key differs, the step runs again, and so does everything that imports it. `go build` doesn't say which input it was. `GODEBUG=gocachehash=1` prints every input of every key, thousands of lines per build, in an order that changes from run to run. `whyrebuilt` lines those inputs up between two runs and tells you which ones changed.

## What you can ask Claude

- "Why does our Go build in GitHub Actions take as long with the cache as without it?"
- "Do these two CI runners share Go build cache entries? Here are two records."
- "Will adding -trimpath fix our cache misses between runners?"
- "Why does every test binary relink when only the version string changed?"

## Install

As a Claude Code plugin:

    claude plugin marketplace add GillMacedo/whyrebuilt
    claude plugin install whyrebuilt@whyrebuilt-marketplace

Or as a CLI with Go 1.22 or newer. It has no dependencies:

    go run github.com/GillMacedo/whyrebuilt/plugin/scripts@v0.1.0 record -o a.rec.gz -- go build ./...

## How to use it

1. Record two runs that should share the cache, with the go command you already run. In CI, record and upload the file:

       - run: go run github.com/GillMacedo/whyrebuilt/plugin/scripts@v0.1.0 record -o build.rec.gz -- go build ./...
       - uses: actions/upload-artifact@v4
         with:
           name: whyrebuilt
           path: build.rec.gz

2. Download the records of two runs, for example two runs of the same commit on different runners, and compare them:

       go run github.com/GillMacedo/whyrebuilt/plugin/scripts@v0.1.0 compare a.rec.gz b.rec.gz

`record` passes stdout through and exits with the command's exit code, so it can replace the build step. The CI step above hasn't been run in GitHub Actions yet.

## Example

`examples/inventory` is a made-up service with four packages. `examples/records/` holds real records of it, made by `scripts/record-examples.sh`: each run copies the module to a new directory, as a CI checkout does, and all runs share one warm `GOCACHE`. Here the second run checked out the same code at another path, as two runners with different workspace directories do. From a clone of this repo:

    $ go run ./plugin/scripts compare examples/records/base.rec.gz examples/records/other-path.rec.gz
    A: examples/records/base.rec.gz (go build ./... in /tmp/whyrebuilt-ci/runner-1/work/inventory)
       go1.27.3, GOCACHE /tmp/whyrebuilt-ci/gocache: 1352 entries before, 1352 after
    B: examples/records/other-path.rec.gz (go build ./... in /tmp/whyrebuilt-ci/runner-2/work/inventory)
       go1.27.3, GOCACHE /tmp/whyrebuilt-ci/gocache: 1352 entries before, 1365 after
    
    Compared 202 steps: 197 with the same key, 5 different.
    
    1. Package directory changed, and the build doesn't use -trimpath: 4 steps, and 1 more that depend on them
       A: /tmp/whyrebuilt-ci/runner-1
       B: /tmp/whyrebuilt-ci/runner-2
       Steps: build example.com/inventory/cmd/inventory, build example.com/inventory/internal/api, build example.com/inventory/internal/store, build example.com/inventory/internal/version
       Because of them: link example.com/inventory/cmd/inventory
       Without -trimpath the compiler writes the absolute source directory into debug info, so the directory is part of the key. Build in the same directory every time, or add -trimpath to both runs.

The standard library keeps its keys, because the go command hides `GOROOT` when compiling it. Only the module's own packages and the link step miss. With `-trimpath` on both runs (`trimpath.rec.gz` and `trimpath-other-path.rec.gz`), every key matches.

A fresh checkout at the same path stays quiet. New file times don't change build keys, because the go command hashes file contents. `--fail` exits 0 here:

    $ go run ./plugin/scripts compare --fail examples/records/base.rec.gz examples/records/fresh-checkout.rec.gz
    A: examples/records/base.rec.gz (go build ./... in /tmp/whyrebuilt-ci/runner-1/work/inventory)
       go1.27.3, GOCACHE /tmp/whyrebuilt-ci/gocache: 1352 entries before, 1352 after
    B: examples/records/fresh-checkout.rec.gz (go build ./... in /tmp/whyrebuilt-ci/runner-1/work/inventory)
       go1.27.3, GOCACHE /tmp/whyrebuilt-ci/gocache: 1352 entries before, 1352 after
    
    Compared 202 steps: 202 with the same key, 0 different.
    Every step has the same key in both runs. B can reuse everything A built, if B's GOCACHE holds A's results.

A job on another Go version shares nothing. The version is the first input of every key, so `whyrebuilt` reports it alone instead of the hundreds of tool IDs and standard library files that change with it:

    $ go run ./plugin/scripts compare examples/records/base.rec.gz examples/records/go1.22.12.rec.gz
    A: examples/records/base.rec.gz (go build ./... in /tmp/whyrebuilt-ci/runner-1/work/inventory)
       go1.27.3, GOCACHE /tmp/whyrebuilt-ci/gocache: 1352 entries before, 1352 after
    B: examples/records/go1.22.12.rec.gz (go build ./... in /tmp/whyrebuilt-ci/runner-1/work/inventory)
       go1.22.12, GOCACHE /tmp/whyrebuilt-ci/gocache: 1997 entries before, 2557 after
    
    Compared 122 steps: 0 with the same key, 122 different.
    Only in A: 80 steps (80 build): build crypto/fips140, build crypto/hkdf, build crypto/hpke, build crypto/internal/constanttime, build crypto/internal/entropy/v1.0.0, and 75 more
    Only in B: 15 steps (15 build): build crypto/internal/alias, build crypto/internal/bigmod, build crypto/internal/edwards25519, build crypto/internal/edwards25519/field, build crypto/internal/nistec, and 10 more
    
    1. Different Go toolchain: 122 steps
       A: go1.27.3
       B: go1.22.12
       Steps: build bufio, build bytes, build cmp, build compress/flate, build compress/gzip, and 117 more
       Every key starts with the go command's version, so nothing built by one version is reused by another. Use the same Go version in both runs, for example with the same setup-go version and a toolchain line in go.mod.

When every key matches and the run still rebuilt, the cache wasn't there. The record counts the cache entries before the run, and says so:

    $ go run ./plugin/scripts compare examples/records/base.rec.gz examples/records/empty-cache.rec.gz | tail -n 4
    Every step has the same key in both runs. B can reuse everything A built, if B's GOCACHE holds A's results.
    
    Note: B's GOCACHE (/tmp/whyrebuilt-ci/empty-cache-19094) was empty when B started, so B rebuilt everything, whatever the keys. The cache wasn't restored, or was restored to another path than go env GOCACHE.
    Note: The runs used different GOCACHE paths (/tmp/whyrebuilt-ci/gocache, then /tmp/whyrebuilt-ci/empty-cache-19094). Matching keys only help if B's cache holds A's results.

Other example pairs in `examples/records/`: `ldflags-1` and `ldflags-2` (a version passed with `-X`), `generated` (a changed generated file), `cgo-cflags` and `cgo-off`, `test` and `test-race`, `vcs-1` and `vcs-2` (two commits with vcs stamping). `examples/README.md` lists how each was made.

### A failed build is refused

A failed build stops early, so its keys would look like a smaller build. `record` keeps the exit code, and `compare` refuses the record. It also refuses a run that exited 0 while its stderr shows a compiler error, as a `|| true` wrapper leaves it:

    $ go run ./plugin/scripts compare examples/records/base.rec.gz examples/records/hidden-failure.rec.gz 2>&1
    whyrebuilt: can't use run B: examples/records/hidden-failure.rec.gz: line 866 is a compiler or vet error: internal/version/broken.go:3:28: cannot use "x" (untyped string constant) as int value in return statement. The build failed even though the command exited 0; a wrapper such as || true may hide the failure
    exit status 2

### Real data: gojq

[gojq](https://github.com/itchyny/gojq) is a jq implementation in Go. `examples/real/gojq/` holds records of two clones of it at different paths, and of two releases. Between v0.12.16 and v0.12.17, three causes show up: the `go` line in `go.mod` moved from 1.20 to 1.21, 15 source files changed, and the link step's default GODEBUG settings lost `panicnil=1`, which Go 1.21 dropped:

    $ go run ./plugin/scripts compare examples/real/gojq/v0.12.16.rec.gz examples/real/gojq/v0.12.17.rec.gz | sed -n 6,12p
    Compared 97 steps: 93 with the same key, 4 different.
    
    1. go line in go.mod changed: 3 steps, and 1 more that depend on them
       A: 1.20
       B: 1.21
       Steps: build github.com/itchyny/gojq, build github.com/itchyny/gojq/cli, build github.com/itchyny/gojq/cmd/gojq
       Because of them: link github.com/itchyny/gojq/cmd/gojq

The 15 changed files match `git diff --name-only v0.12.16 v0.12.17` for the non-test Go files that are built. The dependencies from the module cache keep their keys.

## Usage

    whyrebuilt record -o FILE [--] COMMAND [ARGS...]
    whyrebuilt compare [--fail] [--json] [--limit N] A B

- `record` runs `COMMAND` with `gocachehash=1` added to `GODEBUG`, keeping any settings already there. It saves the inputs of every compile, vet and link key, the command's own stderr messages, the exit code and the number of `GOCACHE` entries before and after. Stdout passes through and the exit code is the command's. A name ending in `.gz` writes gzip.
- `compare` reads two records and explains every key that differs. `--json` prints every step's changed inputs. `--limit` caps each list, 5 by default, 0 for all.
- `--fail` exits 1 when any key differs, a step is in only one run, or a step couldn't be explained.
- Exit code 2 means a record can't be trusted, and nothing is printed on stdout: an empty, garbage or cut-off file, raw `GODEBUG` output, an edited record whose line count doesn't match, a command that failed, compiler or vet errors in stderr, a record with no cache keys, the same record on both sides, or two runs with no step in common.
- For `go test`, record `go test -run '^$' ./...` to build the tests without running them, because a failing test fails the record. Add `-p 1` if the report says some steps were "Not explained": without it, the go command can print a package and its test variant mixed together.

## What it reports

| Cause | Typical reason | What misses |
|---|---|---|
| Package directory changed, no `-trimpath` | checkout path differs between runners or jobs | the module's packages and what imports them |
| Different Go toolchain | another setup-go version or `GOTOOLCHAIN` | everything |
| Compiler version or flags | `-race`, `-cover`, `-gcflags`, `GOFLAGS` | every package |
| `-ldflags` differ, build information stamped into package main | `-X main.version=...`, vcs stamping on a new commit, other build settings | package main and the link |
| `GOROOT` path changed | Go installed at another path, no `-trimpath` | link steps |
| cgo settings | another C compiler version, `CGO_CFLAGS`, `CGO_ENABLED` | `net`, `runtime/cgo`, your cgo packages and what imports them |
| Source files changed | a code change, or a generated file that changes every run | that package and what imports it |
| go line in go.mod, default GODEBUG, PGO profile, target platform, `GOEXPERIMENT`, architecture level | settings in go.mod or the environment | the packages they apply to |
| Dependencies built in only one run | build tags, `CGO_ENABLED` or the package list differ | what imports them |

Anything else is shown as "Other key input differs" with the raw values from the go command.

## Limits

- **Keys, not cache contents.** Matching keys mean B could reuse A's results. Whether B's cache held them depends on how the cache was saved and restored. The record shows the entry count before the run, which catches an empty cache, not a partly restored one.
- **No timings.** The report says which steps had to run, not how long they took.
- **Build steps only.** Test result caching also depends on the files and environment variables the tests read, which this doesn't explain. `GODEBUG=gocachetest=1` prints those lookups.
- **Debug output.** `gocachehash` is a debug setting of the go command, and its format may change. Unknown inputs are shown raw, and a record without keys is refused.
- **Two records needed.** One record can't show what differs. Record both runs with the same command.
- **Records are not private.** They hold package paths, file names, directories and flags, including `-ldflags` values. Check them before uploading from a private project.
- **Mixed output.** In parallel builds, a package and its test variant can be hashed at the same time. Those steps are compared by their keys only, and explained only when recorded with `-p 1`.
- The changed part of a directory is found by splitting on `/`, so Windows paths are shown whole.

## FAQ

**Why does Go rebuild everything in GitHub Actions even with the cache restored?** Either the keys differ or the cache wasn't there. Record two runs and compare them. The most common key differences are a checkout path that differs between runners, another Go version, and different flags between jobs that share a cache.

**Do file modification times break the Go build cache?** Not for builds: the go command hashes file contents for compile keys. A fresh checkout at the same path kept every key in the example. Test result caching does use file times for files the tests open.

**Does -trimpath fix cache misses between runners?** When the cause is the directory or `GOROOT`, yes. In the example, the two checkout paths share every key with `-trimpath`. It also changes the standard library's keys, so the first build with it fills the cache again.

**Why does package main recompile on every commit?** With vcs stamping, the commit and its time are part of package main's build information, which is part of its key. Only package main and the link run again.

**How is this different from sorting and diffing `GODEBUG=gocachehash=1` output?** It pairs each step's inputs between the runs, so the order of the output doesn't matter, groups the changes into causes, and separates root causes from steps that changed only because a dependency's output did. It also refuses failed and cut-off runs, which a plain diff would show as differences.

**Is it an MCP server?** No. It's a skill with one Go source file, so `go run` is all it needs.

## Tested on

- **Example records** of `examples/inventory` for every pair listed above, made on Go 1.27.3 (linux/arm64) with gcc 14.2.0 and a shared cache, plus a run with Go 1.22.12. Each report was checked against the setting that was changed between the two runs.
- **gojq** v0.12.16 and v0.12.17, built in two clones at different paths with `go build ./...` and `go test -run '^$' ./...`, with and without `-p 1`. The changed files were checked against `git diff`.
- **Live runs in `tests/`**: two builds of a small module in one directory and in two directories, a build for another architecture chosen from the machine's own (`runtime.GOARCH`), a failed build, a failing test, a failure hidden by `|| true`, `go version` and `go list`, a command that doesn't exist, and a command that exits 3, which `record` passes on.
- **Broken input** in `tests/`: empty, garbage, missing, a directory, a gzip file cut in half, raw `gocachehash` output, build error text, failed and hidden-failure records, and a record of failing tests, each on either side of `compare`, with and without `--fail` and with `--json`. All exit 2 with nothing on stdout. Unit tests cut a real record at every line end and at regular byte offsets, and drop, add or damage single lines. Every one is refused.
- The tests pass with Go 1.27.3 and with Go 1.22.12 on linux/arm64.

The PGO profile, `GOEXPERIMENT`, architecture level, coverage, fuzzing and `-asmflags` causes follow the go command's source but have no example records yet. It hasn't been tested in a live Claude Code session, on Windows or macOS, in GitHub Actions, with `GOCACHEPROG`, with gccgo, or through `go run ...@v0.1.0`, which needs the published repo.

## Contributing

Contributions are welcome, from people and from people working with AI agents. See [CONTRIBUTING.md](CONTRIBUTING.md). Good first issues are [listed here](https://github.com/GillMacedo/whyrebuilt/issues?q=is%3Aissue+is%3Aopen+label%3A%22good+first+issue%22).

### How pull requests work

1. **Open a PR.** Fork the repo, work on a branch, and keep it to one change. Write a title that says what changes for users, because it becomes the changelog line: `feat:` goes under Added and `fix:` under Fixed.
2. **Automatic check.** Code changes need tests, and `make test` must pass. Changes over 30 lines need a linked open issue (`Closes #<number>`), and a PR can have at most 800 changed lines. If something is missing, a comment tells you what to fix.
3. **Review.** Claude reviews each PR automatically. Code PRs also get a security review and are tested in a sandbox. The review then merges the PR or asks for changes, and a small problem may become a follow-up issue instead. A PR with no new commit 14 days after a change request is closed, and you can reopen it. PRs that change CI, agent settings, npm or Python dependencies, vendored code or build files can't be merged this way, and a comment says so. The same goes for anything the security review doesn't clear.
4. **Release.** A merge that changes what users install becomes a new version within about two hours, with a GitHub Release and, if the project has one, its npm package. Docs and CI changes wait for the next release.

## AI disclosure

This project was largely generated with Claude Code, working on its own, and is published by its maintainer.

## About me

I'm Giulliano Macedo, and I build developer tools at GoTech Solutions. Follow along or get in touch:

- GitHub: [GillMacedo](https://github.com/GillMacedo)
- LinkedIn: [giulliano](https://www.linkedin.com/in/giulliano/)
- X: [@giumacedo](https://x.com/giumacedo)

If `whyrebuilt` saved you some prompts, a star helps others find it.

---

Built by [Giulliano Macedo](https://github.com/GillMacedo), GoTech Solutions · [LinkedIn](https://www.linkedin.com/in/giulliano/) · [X](https://x.com/giumacedo)
