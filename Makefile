.PHONY: backend-build backend-test frontend-install frontend-test frontend-build chart-deps chart-test test

HELM ?= helm

backend-build:
	$(MAKE) -C backend build

backend-test:
	cd backend && go test ./...

frontend-install:
	cd frontend && bun install --frozen-lockfile

frontend-test:
	cd frontend && bun run type-check && bun run test

frontend-build:
	cd frontend && bun run build

chart-deps:
	$(HELM) dependency build backend/helm/agentapi-proxy
	$(HELM) dependency build chart/ccplant

chart-test: chart-deps
	$(HELM) lint backend/helm/agentapi-proxy --strict
	$(HELM) template backend backend/helm/agentapi-proxy >/dev/null
	$(HELM) lint frontend/helm/agentapi-ui --strict
	$(HELM) template frontend frontend/helm/agentapi-ui >/dev/null
	$(HELM) lint chart/ccplant --strict
	$(HELM) template ccplant chart/ccplant >/dev/null
	HELM=$(HELM) ./scripts/test-helm-render.sh

test: backend-test frontend-test chart-test
