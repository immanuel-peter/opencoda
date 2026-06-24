# OpenCoda E2E validation (requires cluster + creds)

.PHONY: e2e-static
e2e-static:
	@echo "helm upgrade --install opencoda charts/opencoda -n opencoda-system --create-namespace"
	@echo "kubectl apply -f test/e2e/fixtures/"

.PHONY: e2e-aws
e2e-aws:
	@echo "Provision AWS spot GPU pool and scale endpoint 0->1"
