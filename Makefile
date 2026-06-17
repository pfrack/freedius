PKGS := $(shell go list ./... 2>/dev/null)
ARGS ?=
HOOKS_DIR := .git/hooks

.PHONY: test vet build ci tidy run lint lint-static lint-golangci manual-test install-hooks

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