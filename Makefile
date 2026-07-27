.PHONY: bootstrap test test-race test-integration invariant-test contract-test e2e fuzz local-up migrate-up migrate-down run \
	load-smoke load-lunchrush load-spike load-breakpoint load-soak \
	compose-up compose-down kind-up kind-down helm-up keda-up tf-aws-lab-up tf-aws-lab-down \
	chaos replay benchmark-report \
	cloud-plan cloud-up cloud-destroy portability-test cloud-failover

DATABASE_URL ?= postgres://dispatch:dispatch@localhost:5432/dispatch?sslmode=disable
KAFKA_BROKERS ?= localhost:19092
BASE_URL ?= http://localhost:8080
SEED ?= 20260717
SCALE ?= 1
DURATION ?= 5m
PROVIDER ?= cloud-a
TOPIC ?= dispatch.delivery-events.dlq

bootstrap:
	go mod tidy

test:
	go test ./...

test-race:
	go test -race ./...

test-integration:
	DATABASE_URL=$(DATABASE_URL) KAFKA_BROKERS=$(KAFKA_BROKERS) go test -tags=integration ./test/integration/...

# test/invariant: propriedades centrais do domínio (autorização por dono,
# entregador nunca com duas entregas ativas, epoch de fencing nunca
# regride, log de transições segue a máquina de estados), rotuladas e
# centralizadas, ver test/invariant/main_test.go.
invariant-test:
	DATABASE_URL=$(DATABASE_URL) go test -tags=integration -race ./test/invariant/...

# test/contract: documentação (api/openapi, contracts/asyncapi) contra
# código real (rotas HTTP registradas, payload real do outbox).
contract-test:
	DATABASE_URL=$(DATABASE_URL) go test -tags=integration -race ./test/contract/...

# test/e2e: jornada completa via HTTP real contra os serviços de verdade.
# Pré-requisito: `make compose-up` (ou `docker compose --profile app up -d
# --build`) com o stack saudável.
e2e:
	go test -tags=e2e -race ./test/e2e/...

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

# perfil de spike (3x): rampa até 10 VUs, sustenta, salta para 30 (3x) por
# 10s, sustenta, volta a 0. Já existia como docs/benchmarks/tier-4-load/
# (evidência real do tier 4); este target só amarra o comando.
load-spike:
	k6 run -e BASE_URL=$(BASE_URL) loadtest/k6/tier-4-steady-spike.js

# taxa de chegada crescente até violar um SLO com segurança (stop
# condition via threshold abortOnFail). Ver docs/benchmarks/breakpoint.md
# para a execução real já registrada.
load-breakpoint:
	k6 run -e BASE_URL=$(BASE_URL) loadtest/k6/breakpoint.js

# carga baixa e constante por DURATION (default 5m), procurando
# degradação lenta. Ver docs/benchmarks/tier-5-soak/ para a corrida longa
# equivalente já feita com o LunchRush.
load-soak:
	k6 run -e BASE_URL=$(BASE_URL) -e DURATION=$(DURATION) loadtest/k6/soak.js

compose-up:
	docker compose --profile app --profile observability up -d --build

compose-down:
	docker compose --profile app --profile observability down

kind-up:
	bash scripts/kind-deploy.sh

kind-down:
	kind delete cluster --name dispatch

# Tier 4: chart Helm no lugar de Kustomize (ver docs/adr/0013), KEDA
# escalando por lag (ver docs/adr/0014) e Terraform contra LocalStack (ver
# docs/adr/0012). kind-up/kind-down acima continuam existindo só como
# registro do que o tier 3 usava (deploy/kubernetes/), congelado.
helm-up:
	bash scripts/helm-deploy.sh

keda-up:
	bash scripts/keda-install.sh

tf-aws-lab-up:
	docker compose --profile aws-lab up -d localstack
	cd infra/terraform/environments/aws-lab && terraform init && terraform apply -auto-approve

tf-aws-lab-down:
	cd infra/terraform/environments/aws-lab && terraform destroy -auto-approve
	docker compose --profile aws-lab stop localstack

# dispara um cenário de chaos/local/ pelo nome do arquivo sem extensão,
# ex: `make chaos SCENARIO=redis-unavailable`. Ver chaos/local/README.md
# para a lista completa e os pré-requisitos de cada um.
chaos:
	@if [ -z "$(SCENARIO)" ]; then echo "uso: make chaos SCENARIO=<nome> (ver chaos/local/)"; exit 1; fi
	bash chaos/local/$(SCENARIO).sh

# replay de uma mensagem específica de uma DLQ pelo offset, ver
# docs/runbooks/dlq-replay.md e scripts/dlq-replay.sh.
replay:
	@if [ -z "$(DLQ_ID)" ]; then echo "uso: make replay TOPIC=dispatch.delivery-events.dlq DLQ_ID=<offset>"; exit 1; fi
	bash scripts/dlq-replay.sh $(TOPIC) $(DLQ_ID)

# agrega os benchmarks mais recentes de docs/benchmarks/ num resumo único.
benchmark-report:
	bash scripts/benchmark-report.sh

# Tier 6: um root Terraform e um stack docker compose por provedor
# simulado (cloud-a = docker-compose.yml, cloud-b = docker-compose.cloud-b.yml),
# ver docs/adr/0021 e docs/benchmarks/tier-6-portability/. PROVIDER default
# é cloud-a.
cloud-plan:
	cd infra/terraform/environments/$(PROVIDER) && terraform init && terraform plan

cloud-up:
	@if [ "$(PROVIDER)" = "cloud-b" ]; then \
		docker compose -f docker-compose.cloud-b.yml --profile aws-lab up -d localstack; \
	else \
		docker compose --profile aws-lab up -d localstack; \
	fi
	cd infra/terraform/environments/$(PROVIDER) && terraform init && terraform apply -auto-approve

cloud-destroy:
	cd infra/terraform/environments/$(PROVIDER) && terraform destroy -auto-approve
	@if [ "$(PROVIDER)" = "cloud-b" ]; then \
		docker compose -f docker-compose.cloud-b.yml --profile aws-lab stop localstack; \
	else \
		docker compose --profile aws-lab stop localstack; \
	fi

# smoke k6 contra os dois stacks simulados, provando que o mesmo cliente
# fala com os dois sem mudança de código (só BASE_URL). Ver
# docs/benchmarks/tier-6-portability/k6-smoke-cloud-a.json e -cloud-b.json
# para a evidência já registrada; requer os dois stacks no ar
# (`make cloud-up PROVIDER=cloud-a` e `... PROVIDER=cloud-b`, mais
# `docker compose --profile app up -d --build` de cada lado).
portability-test:
	k6 run -e BASE_URL=http://localhost:8083 --summary-export=/tmp/k6-smoke-cloud-a.json loadtest/k6/smoke.js
	k6 run -e BASE_URL=http://localhost:18083 --summary-export=/tmp/k6-smoke-cloud-b.json loadtest/k6/smoke.js

# encadeia cmd/cloudfailover: seed/promote/assign em cloud-a, backup
# lógico, restauração e promoção em cloud-b, prova que o writer antigo é
# rejeitado. Ver scripts/cloud-failover-demo.sh e
# docs/benchmarks/tier-6-portability/failover-transcript.txt para a
# sequência já executada e documentada.
cloud-failover:
	bash scripts/cloud-failover-demo.sh
