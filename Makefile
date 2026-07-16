# Makefile for manglekit-examples
#
# Targets:
#   build         — build all examples
#   test          — run all example tests
#   vet           — run go vet on all examples
#   clean         — clean test caches and binaries
#   run/<name>    — run a specific example (e.g. make run/code_to_policy_extractor)
#
# Requires: Go 1.24+, manglekit as a sibling at ../manglekit

GO ?= go

.PHONY: build test vet clean run/%

build:
	$(GO) build ./...

test:
	$(GO) test ./... -count=1

vet:
	$(GO) vet ./...

clean:
	$(GO) clean ./...
	rm -f */main */hybrid_rag */*.test */*.out */*.cov

# Run a specific example: make run/code_to_policy_extractor
run/%:
	$(GO) run ./$*/
