.PHONY: bootstrap test test-race test-integration fuzz local-up

bootstrap:
	go mod tidy

test:
	go test ./...

test-race:
	go test -race ./...

test-integration:
	go test -tags=integration ./test/integration/...

fuzz:
	go test -fuzz=. -fuzztime=30s ./internal/delivery/...

local-up:
	@echo "ainda não existe compose local: item pendente no backlog do tier 1"
