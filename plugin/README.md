# whyrebuilt plugin

This plugin adds the `explaining-go-build-cache-misses` skill to Claude Code. When you ask why a Go build or `go test` run keeps rebuilding, especially in CI with a restored `GOCACHE`, Claude runs the bundled `scripts/whyrebuilt.go` with `go run`. It records a run with `GODEBUG=gocachehash=1`, compares the cache keys of two runs, and names the inputs that changed: the checkout path, the Go version, compiler or linker flags, cgo settings, build stamps or source files. Claude then points at the CI setting or file behind each cause. It needs Go 1.22 or newer. See the repository README for examples and limits.

Install it with `claude plugin marketplace add GillMacedo/whyrebuilt` and then `claude plugin install whyrebuilt@whyrebuilt-marketplace`.
