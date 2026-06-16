PKGS := $(shell go list ./... 2>/dev/null)

.PHONY: test vet build ci tidy run

test:
	@if [ -n "$(PKGS)" ]; then go test -race -cover $(PKGS); else echo "no packages to test"; fi

vet:
	@if [ -n "$(PKGS)" ]; then go vet $(PKGS); else echo "no packages to vet"; fi

build:
	go build -o freedius .

tidy:
	go mod tidy

ci: vet test build

run:
	./test-manual.sh
