# Examples

All records here are real output of `whyrebuilt record` from Go 1.27.3 on linux/arm64, unless noted. Records hold package paths, file names, directories, flags and hashes. They don't contain source code.

## inventory

`inventory/` is a made-up service: a store of item counts, an HTTP API over it, a version package and a main package. It uses only the standard library. Through `net/http` it also builds the standard library's cgo packages (`net`, `runtime/cgo`) when cgo is on.

## records

`scripts/record-examples.sh` (`make records`) made every file in `records/`. Each run copies `inventory/` to a new directory, the way a CI checkout does, so file times are new every time. All runs share one `GOCACHE` in `/tmp/whyrebuilt-ci/gocache`, warmed first. `runner-1` and `runner-2` are two checkout paths.

| Record | Command and setting |
|---|---|
| `base` | `go build ./...` in `runner-1` |
| `fresh-checkout` | the same, after a new copy at the same path |
| `other-path` | the same in `runner-2` |
| `trimpath`, `trimpath-other-path` | `go build -trimpath ./...` in `runner-1` and `runner-2` |
| `ldflags-1`, `ldflags-2` | `go build -ldflags "-X .../version.Version=build-1"` and `build-2` |
| `generated` | `version.go` with two extra lines, as a generator that writes a build number would leave it |
| `go1.22.12` | `GOTOOLCHAIN=go1.22.12 go build ./...` |
| `cgo-cflags` | `CGO_CFLAGS="-O2 -g -fno-omit-frame-pointer" go build ./...` (gcc 14.2.0) |
| `cgo-off` | `CGO_ENABLED=0 go build ./...` |
| `empty-cache` | `go build ./...` with a `GOCACHE` that doesn't exist yet |
| `test`, `test-other-path` | `go test ./...` in `runner-1` and `runner-2` |
| `test-race` | `go test -race ./...` |
| `vcs-1`, `vcs-2` | `go build ./...` in a git repository, at two commits |
| `failed-build` | `go build ./...` with a type error added; exits 1 |
| `hidden-failure` | `sh -c 'go build ./... \|\| true'` with the same error; exits 0 |

## real/gojq

[gojq](https://github.com/itchyny/gojq) (MIT license) is a jq implementation in Go. The records come from two clones of it, in `/tmp/real/p1/gojq` and `/tmp/real/p2/gojq`, with the user's normal `GOCACHE`:

| Record | Clone | Command |
|---|---|---|
| `v0.12.16` | p1 at v0.12.16 (`0607aa5`) | `go build ./...` |
| `v0.12.17` | p1 at v0.12.17 (`f4c2cfe`) | `go build ./...` |
| `v0.12.17-other-path` | p2 at v0.12.17 | `go build ./...` |
| `test`, `test-other-path` | p1 and p2 at v0.12.17 | `go test -run '^$' ./...` |
| `test-p1`, `test-p1-other-path` | p1 and p2 at v0.12.17 | `go test -p 1 -run '^$' ./...` |
| `test-failing` | p1 at v0.12.17 | `go test ./...`, which fails on Go 1.27.3 because two expected error messages changed; exits 1 |

The changed files `compare v0.12.16.rec.gz v0.12.17.rec.gz` lists were checked against `git diff --name-only v0.12.16 v0.12.17`: all 15 non-test Go files that are built match. `debug.go` also changed, and has a `gojq_debug` build tag, so it isn't built and isn't listed.
