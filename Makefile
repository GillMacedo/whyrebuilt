.PHONY: test lint demo records

RUN = go run ./plugin/scripts
R = examples/records

test:
	test -z "$$(gofmt -l plugin tests examples)"
	go vet ./...
	cd examples/inventory && go vet ./...
	go test -count=1 ./...

lint:
	claude plugin validate ./plugin --strict
	claude plugin validate . --strict

# The second pair differs on purpose, so it runs without --fail.
demo:
	$(RUN) compare --fail $(R)/base.rec.gz $(R)/fresh-checkout.rec.gz
	$(RUN) compare $(R)/base.rec.gz $(R)/other-path.rec.gz

# Records new example runs. They differ on every run, so update README.md after.
records:
	scripts/record-examples.sh
