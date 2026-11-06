#!/bin/sh
# Records the example runs in examples/records/ from examples/inventory.
# Usage: scripts/record-examples.sh (or make records)
# Each run copies the module to a fresh directory, the way a CI checkout
# does, so file modification times are new every time. Records differ on
# every run (times, cache entry counts), so update the README afterwards.
# Needs Go, gcc for the cgo and race runs, git for the vcs runs, and the
# go1.22.12 toolchain for the toolchain run (GOTOOLCHAIN downloads it once).
set -eu
repo=$(cd "$(dirname "$0")/.." && pwd)
out="$repo/examples/records"
work=/tmp/whyrebuilt-ci
bin="$work/whyrebuilt"
mkdir -p "$out" "$work"
(cd "$repo" && go build -o "$bin" ./plugin/scripts)
export GOCACHE="$work/gocache"
export GOFLAGS=
export GOTOOLCHAIN=local

checkout() { # checkout DIR: a fresh copy of the example module
  rm -rf "$1"
  mkdir -p "$(dirname "$1")"
  cp -R "$repo/examples/inventory" "$1"
}
record() { # record NAME DIR COMMAND...
  name=$1 dir=$2
  shift 2
  (cd "$dir" && "$bin" record -o "$out/$name.rec.gz" -- "$@" > /dev/null)
}

a="$work/runner-1/work/inventory"
b="$work/runner-2/work/inventory"

# Warm the cache, then the healthy pair: a fresh checkout at the same path.
checkout "$a"; (cd "$a" && go build ./... && go test ./... > /dev/null)
checkout "$a"; record base "$a" go build ./...
checkout "$a"; record fresh-checkout "$a" go build ./...

# Same code, checked out at another path.
checkout "$b"; record other-path "$b" go build ./...

# The same two paths with -trimpath.
checkout "$a"; record trimpath "$a" go build -trimpath ./...
checkout "$b"; record trimpath-other-path "$b" go build -trimpath ./...

# A version stamped with -ldflags that changes on every run.
checkout "$a"; record ldflags-1 "$a" go build -ldflags "-X example.com/inventory/internal/version.Version=build-1" ./...
checkout "$a"; record ldflags-2 "$a" go build -ldflags "-X example.com/inventory/internal/version.Version=build-2" ./...

# A generated file that changes on every run.
checkout "$a"; printf '\n// Generated at build 2.\nconst Generated = "2"\n' >> "$a/internal/version/version.go"
record generated "$a" go build ./...

# Another Go toolchain.
checkout "$a"; GOTOOLCHAIN=go1.22.12 record go1.22.12 "$a" go build ./...

# cgo settings: other CGO_CFLAGS, and cgo turned off.
checkout "$a"; CGO_CFLAGS="-O2 -g -fno-omit-frame-pointer" record cgo-cflags "$a" go build ./...
checkout "$a"; CGO_ENABLED=0 record cgo-off "$a" go build ./...

# A restored cache with matching keys, and a cache that wasn't restored.
checkout "$a"; GOCACHE="$work/empty-cache-$$" record empty-cache "$a" go build ./...
rm -rf "$work/empty-cache-$$"

# go test in two checkouts at different paths, and with -race.
checkout "$a"; record test "$a" go test ./...
checkout "$b"; record test-other-path "$b" go test ./...
checkout "$a"; record test-race "$a" go test -race ./...

# vcs stamping: the same tree at two commits.
checkout "$a"
(cd "$a" && git init -q && git add . && git -c user.name=demo -c user.email=demo@example.com commit -qm one && git tag one)
record vcs-1 "$a" go build ./...
(cd "$a" && echo "# notes" > NOTES.md && git add . && git -c user.name=demo -c user.email=demo@example.com commit -qm two)
record vcs-2 "$a" go build ./...
rm -rf "$a/.git"

# A build that fails, and the same failure hidden behind "|| true".
checkout "$a"; printf 'package version\n\nfunc Broken() int { return "x" }\n' > "$a/internal/version/broken.go"
record failed-build "$a" go build ./... 2> /dev/null || true
record hidden-failure "$a" sh -c 'go build ./... || true' 2> /dev/null

rm -rf "$work/runner-1" "$work/runner-2"
ls -l "$out"
