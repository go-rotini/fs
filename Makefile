.PHONY: all clean lint test test-acceptance test-bench \
        test-conformance test-fuzz test-mutation test-race

all: clean lint test test-acceptance test-bench test-fuzz test-mutation test-race

clean:
	@rm -rf *.out test_mutation.json

lint:
	@gofmt_unformatted=$$(gofmt -l . 2>/dev/null | grep -v '^testdata/' || true); \
	test -z "$$gofmt_unformatted" || (echo "files not formatted:" && echo "$$gofmt_unformatted" && exit 1)
	go vet ./...
	go mod verify
	go tool golangci-lint run ./...
	go tool go-licenses check ./...
	go tool govulncheck ./...

test:
	@go test -v -count=1 -coverprofile=test.out ./...
	@go tool cover -func=test.out

test-acceptance:
	@go test -v -count=1 -run TestAcceptance -coverprofile=test_acceptance.out ./...
	@go tool cover -func=test_acceptance.out

test-bench:
	@go test -bench=. -benchmem -count=1 ./... | tee test_bench.out

test-conformance:
	@go test -v -count=1 -run TestConformance -coverprofile=test_conformance.out ./...
	@go tool cover -func=test_conformance.out

test-fuzz:
	@go test -fuzz=FuzzExpand -fuzztime=60s -run=^$$ .
	@go test -fuzz=FuzzIsSubpath -fuzztime=60s -run=^$$ .
	@go test -fuzz=FuzzMatch -fuzztime=60s -run=^$$ .
	@go test -fuzz=FuzzParseBytes -fuzztime=60s -run=^$$ .
	@go test -fuzz=FuzzSanitizeFilename -fuzztime=60s -run=^$$ .
	@go test -fuzz=FuzzMagic -fuzztime=60s -run=^$$ .

test-mutation:
	@go tool github.com/go-gremlins/gremlins/cmd/gremlins unleash --config .gremlins.yaml

test-race:
	@go test -race -count=1 -coverprofile=test_race.out ./...
	@go tool cover -func=test_race.out
