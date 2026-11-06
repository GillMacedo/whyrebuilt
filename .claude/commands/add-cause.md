Add an explanation for a cache key input that whyrebuilt reports as "Other key input differs": $ARGUMENTS

1. Record two real runs that differ only in that input with `go run ./plugin/scripts record -o FILE.rec.gz -- go build ./...`, using `examples/inventory` or an open-source project whose license you note in `examples/README.md`.
2. Find where the go command writes that input in `$(go env GOROOT)/src/cmd/go/internal/work/exec.go` and link the line in the pull request.
3. Add a case to `slot` and `describe` in `plugin/scripts/whyrebuilt.go`, with a title and advice that only say what the source and the records show.
4. Add the records to `examples/records/`, a test in `tests/integration_test.go`, and a row to the README table. Run `make test`.
