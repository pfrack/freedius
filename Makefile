PKGS := $(shell go list ./... 2>/dev/null)
ARGS ?=
HOOKS_DIR := .git/hooks

.PHONY: test vet build ci tidy run lint lint-static lint-golangci manual-test install-hooks format format-changed install-goimports install-golines install-gci

test:
	@if [ -n "$(PKGS)" ]; then go test -race -cover $(PKGS); else echo "no packages to test"; fi

vet:
	@if [ -n "$(PKGS)" ]; then go vet $(PKGS); else echo "no packages to vet"; fi

build:
	go build -o freedius .

tidy:
	go mod tidy

run:
	go run . $(ARGS)

verbose:
	go run .  --verbose-errors


lint-static:
	@which staticcheck > /dev/null 2>&1 || (echo "Installing staticcheck..." && go install honnef.co/go/tools/cmd/staticcheck@latest)
	staticcheck $(PKGS)

lint-golangci:
	@which golangci-lint > /dev/null 2>&1 || (echo "golangci-lint not found. Install: https://golangci-lint.run/usage/install/" && exit 1)
	golangci-lint run ./...

lint: vet lint-static lint-golangci

ci: lint test build

manual-test:
	./test-manual.sh

install-hooks:
	@echo "Installing git hooks..."
	@cp scripts/pre-commit $(HOOKS_DIR)/pre-commit
	@chmod +x $(HOOKS_DIR)/pre-commit
	@echo "Done. Hook installed at $(HOOKS_DIR)/pre-commit"

GCI_SECTIONS := --skip-generated -s standard -s default -s "prefix(github.com/pfrack/freedius)" -s blank -s dot -s alias -s localmodule

format: install-goimports install-golines install-gci
	@find . -name "*.go" -exec sh -c '\
		gofmt -w "$$1" && \
		goimports -w -local github.com/pfrack/freedius "$$1" && \
		golines -w "$$1" && \
		gci write $(GCI_SECTIONS) "$$1"' sh {} \;

CHANGED_GO_FILES := $(shell git diff --name-only --diff-filter=ACM HEAD 2>/dev/null | grep '\.go$$'; git ls-files --others --exclude-standard | grep '\.go$$')

format-changed: install-goimports install-golines install-gci
	@if [ -z "$(CHANGED_GO_FILES)" ]; then \
		echo "No changed Go files."; \
	else \
		echo "Formatting:" $(CHANGED_GO_FILES); \
		for f in $(CHANGED_GO_FILES); do \
			gofmt -w "$$f" && \
			goimports -w -local github.com/pfrack/freedius "$$f" && \
			golines -w "$$f" && \
			gci write $(GCI_SECTIONS) "$$f"; \
		done; \
	fi

install-goimports:
	@command -v goimports >/dev/null 2>&1 || go install golang.org/x/tools/cmd/goimports@latest

install-golines:
	@command -v golines >/dev/null 2>&1 || go install github.com/segmentio/golines@latest

install-gci:
	@command -v gci >/dev/null 2>&1 || go install github.com/daixiang0/gci@latest