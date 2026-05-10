.PHONY: all clean lint deps-check test test-acceptance test-bench \
        test-conformance test-fuzz test-mutation test-race

all: clean lint test test-acceptance test-bench test-fuzz test-mutation test-race

clean:
	@rm -rf *.out test_mutation.json

lint: deps-check
	@gofmt_unformatted=$$(gofmt -l . 2>/dev/null | grep -v '^testdata/' || true); \
	test -z "$$gofmt_unformatted" || (echo "files not formatted:" && echo "$$gofmt_unformatted" && exit 1)
	go vet ./...
	go mod verify
	go tool golangci-lint run ./...
	go tool go-licenses check ./...
	go tool govulncheck ./...

# deps-check enforces the package's defining constraint: zero non-stdlib
# runtime dependencies in any importable package. CI fails the build on
# any third-party import in the runtime graph. Tool-only deps (the `tool`
# directives in go.mod) are not part of `go list -deps` output and are
# therefore unaffected.
deps-check:
	@stdlib=$$(go list std 2>/dev/null | tr '\n' '|' | sed 's/|$$//'); \
	deps=$$(go list -deps ./... 2>/dev/null | grep -vE "^($$stdlib)$$" | grep -v "^github.com/go-rotini/fs"); \
	test -z "$$deps" || (echo "non-stdlib runtime deps detected:" && echo "$$deps" && exit 1)

test:
	@go test -v -count=1 -coverprofile=test.out ./...
	@go tool cover -func=test.out | tail -1

test-acceptance:
	@go test -v -count=1 -run TestAcceptance -coverprofile=test_acceptance.out ./...
	@go tool cover -func=test_acceptance.out | tail -1

test-bench:
	@go test -bench=. -benchmem -count=1 ./... | tee test_bench.out

test-conformance:
	@go test -v -count=1 -run TestConformance -coverprofile=test_conformance.out ./...
	@go tool cover -func=test_conformance.out | tail -1

test-fuzz:
	@go test -fuzz=FuzzExpand -fuzztime=60s -run=^$$ .
	@go test -fuzz=FuzzIsSubpath -fuzztime=60s -run=^$$ .
	@go test -fuzz=FuzzGlob -fuzztime=60s -run=^$$ .
	@go test -fuzz=FuzzParseBytes -fuzztime=60s -run=^$$ .
	@go test -fuzz=FuzzWalkOptions -fuzztime=60s -run=^$$ .
	@go test -fuzz=FuzzSanitizeFilename -fuzztime=60s -run=^$$ .
	@go test -fuzz=FuzzWatcher -fuzztime=60s -run=^$$ ./watcher

test-mutation:
	@go tool github.com/go-gremlins/gremlins/cmd/gremlins unleash --config .gremlins.yaml

test-race:
	@go test -race -count=1 -coverprofile=test_race.out ./...
	@go tool cover -func=test_race.out | tail -1
