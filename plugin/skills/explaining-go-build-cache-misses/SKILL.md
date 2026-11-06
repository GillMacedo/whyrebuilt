---
name: explaining-go-build-cache-misses
description: Explains why a Go build, go test or go vet run missed the Go build cache (GOCACHE), by recording two runs with GODEBUG=gocachehash=1 and comparing their cache keys. Names each input that changed, such as the checkout directory without -trimpath, the Go version, -gcflags or -race, -ldflags, cgo settings, vcs stamping, GOROOT or a source file, and separates root causes from packages that changed only because a dependency did. Use when the user asks why Go rebuilds everything in CI even though the build cache is restored, why setup-go or actions/cache caching doesn't speed up builds, why two machines or two jobs don't share cache entries, or wants to check that a CI change keeps the cache warm.
---

# Explaining Go build cache misses

The go command looks up each compile, vet and link step in `GOCACHE` by a key hashed from that step's inputs. If any input differs between two runs, the step and everything that imports it runs again, even with the cache restored. The bundled tool records the inputs of every key in a run and compares two runs. It needs Go and runs the user's own go command; it never changes files in the project.

## Record two runs

Each run is recorded with the command the user already runs, from the module directory:

    go run "${CLAUDE_PLUGIN_ROOT}/scripts/whyrebuilt.go" record -o a.rec.gz -- go build ./...

- The record keeps the command's exit code, so a failed build stays failed. Stdout passes through.
- For `go test`, record `go test -run '^$' ./...` to compile and link the tests without running them. A failing test makes the record unusable.
- Add `-p 1` to the go command when the comparison says some steps were "Not explained". Without it, the go command can print a package and its test variant mixed together.
- To find out why CI misses the cache, record in CI and download the files. Suggest a step like this, and an artifact upload of the file:

      - run: go run github.com/GillMacedo/whyrebuilt/plugin/scripts@v0.1.0 record -o build.rec.gz -- go build ./...

  Two CI runs of the same commit are the cleanest pair. A CI run and a local run show every difference between the machines, which is usually more than the cache problem.
- To test a theory locally, record a run, change one thing (another directory, a flag, an environment variable), and record again.

## Compare

    go run "${CLAUDE_PLUGIN_ROOT}/scripts/whyrebuilt.go" compare a.rec.gz b.rec.gz

Add `--json` for every changed input of every step, and `--limit 0` to list every step. `--fail` exits 1 when anything differs, for use in CI.

Exit code 2 means a record can't be trusted: say why, from the message, and don't guess at a result. Common causes are a failed command, a file cut off during upload, raw `GODEBUG` output instead of a record, and the same file given twice.

## Read the report

Each numbered cause is an input that changed in some steps ("Steps"), with the steps that changed only because they import one of those ("Because of them"). Fix the cause with the most steps first. Typical fixes:

1. **Package directory changed, and the build doesn't use -trimpath.** CI checks out to a path that differs between runs or runners. Use the same path, or add `-trimpath` to every build, test and vet command, for example with `GOFLAGS=-trimpath`.
2. **Different Go toolchain.** Pin the same version in every job: the same `go-version` in setup-go, or a `toolchain` line in `go.mod`. Nothing is shared across Go versions.
3. **GOROOT path changed.** Only link steps miss. Go installed at a different path each time; `-trimpath` also fixes it.
4. **-ldflags differ** and **build information stamped into package main.** A version or date passed with `-X`, or vcs stamping after a new commit. These recompile only package main and relink, which is usually fine. Mention `-buildvcs=false` only if the stamp isn't needed.
5. **Compiler flags differ.** `-race`, `-cover`, `-gcflags` or `GOFLAGS` differ between jobs. Jobs with different flags never share entries, so give each its own cache key.
6. **cgo settings differ.** Another C compiler version or `CGO_CFLAGS`. Pin the image or set the variables the same way.
7. **Source files changed.** Expected after a code change. If the same file changes on every run without a code change, look for a generator that writes a timestamp or random value.

If every step has the same key and the run still rebuilt, the cache wasn't there: look at the note about B's `GOCACHE`. An empty or unreadable cache, a cache restored to a path other than `go env GOCACHE`, or a cache key that never matches in `actions/cache` are the usual reasons.

## Report

1. Say how many steps differ out of how many were compared, and name the root causes in order of size.
2. For each cause, give the A and B values and the one setting to change in the user's CI or build file. Show the edit if the file is in the repo.
3. Say what the records can't show: whether B's cache actually held A's entries, and test result caching, which also depends on files and environment variables the tests read (`GODEBUG=gocachetest=1` shows those).

## Don't

- Don't claim a time saving. The tool shows which keys differ, not how long each step takes.
- Don't call the cache fine because every key matches. Matching keys only help if B's cache holds A's results.
- Don't edit CI files or delete caches without asking.
