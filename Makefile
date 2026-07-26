.PHONY: bootstrap test test-race test-integration fuzz local-up migrate-up migrate-down run load-smoke load-lunchrush compose-up compose-down kind-up kind-down

DATABASE_URL ?= postgres://dispatch:dispatch@localhost:5432/dispatch?sslmode=disable
KAFKA_BROKERS ?= localhost:19092
BASE_URL ?= http://localhost:8080
SEED ?= 20260717
SCALE ?= 1

bootstrap:
	go mod tidy

test:
	go test ./...

test-race:
	go test -race ./...

test-integration:
	DATABASE_URL=$(DATABASE_URL) KAFKA_BROKERS=$(KAFKA_BROKERS) go test -tags=integration ./test/integration/...

fuzz:
	go test -fuzz=. -fuzztime=30s ./internal/delivery/...

local-up:
	docker compose up -d postgres
	DATABASE_URL=$(DATABASE_URL) go run ./cmd/migrate up

migrate-up:
	DATABASE_URL=$(DATABASE_URL) go run ./cmd/migrate up

migrate-down:
	DATABASE_URL=$(DATABASE_URL) go run ./cmd/migrate down

run:
	DATABASE_URL=$(DATABASE_URL) go run ./cmd/delivery-api

load-smoke:
	k6 run -e BASE_URL=$(BASE_URL) loadtest/k6/smoke.js

load-lunchrush:
	go run ./cmd/lunchrush -base-url $(BASE_URL) -seed $(SEED) \
		-orders $$(( 200 * $(SCALE) )) -couriers $$(( 20 * $(SCALE) )) -concurrency 20 \
		-out lunchrush-report

compose-up:
	docker compose --profile app --profile observability up -d --build

compose-down:
	docker compose --profile app --profile observability down

kind-up:
	bash scripts/kind-deploy.sh

kind-down:
	kind delete cluster --name dispatch
