# Image URL to use all building/pushing image targets
IMG ?= opencoda:latest
REGISTRY ?= ghcr.io/immanuel-peter/opencoda

# Tool versions
CONTROLLER_GEN_VERSION ?= v0.21.0
ENVTEST_K8S_VERSION ?= 1.36.0

# Get the currently used golang install path
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

CONTROLLER_GEN = $(LOCALBIN)/controller-gen
$(CONTROLLER_GEN): $(LOCALBIN)
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen,$(CONTROLLER_GEN_VERSION))

.PHONY: all
all: generate manifests build test

.PHONY: generate
generate: $(CONTROLLER_GEN)
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./api/..."

.PHONY: manifests
manifests: $(CONTROLLER_GEN)
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd:allowDangerousTypes=true webhook paths="./api/..." paths="./internal/webhook/..." output:crd:artifacts:config=config/crd/bases output:webhook:artifacts:config=config/webhook

.PHONY: build
build: generate
	go build -o bin/coda-controller-manager ./cmd/coda-controller-manager
	go build -o bin/coda-gateway ./cmd/coda-gateway
	go build -o bin/coda-node-agent ./cmd/coda-node-agent
	go build -o bin/coda-webhook ./cmd/coda-webhook
	go build -o bin/coda ./cmd/coda

.PHONY: test
test:
	go test ./pkg/... ./internal/... -count=1

.PHONY: e2e-kind
e2e-kind:
	chmod +x hack/e2e-kind.sh && ./hack/e2e-kind.sh

.PHONY: e2e-aws
e2e-aws:
	chmod +x hack/e2e-aws.sh && ./hack/e2e-aws.sh

.PHONY: e2e-eks
e2e-eks:
	chmod +x hack/e2e-eks.sh && ./hack/e2e-eks.sh

.PHONY: e2e-eks-gpu
e2e-eks-gpu:
	chmod +x hack/e2e-eks-gpu.sh && ./hack/e2e-eks-gpu.sh

.PHONY: e2e-eks-vllm
e2e-eks-vllm:
	chmod +x hack/e2e-eks-vllm.sh && ./hack/e2e-eks-vllm.sh

.PHONY: e2e-uc1
e2e-uc1:
	chmod +x hack/e2e-uc1.sh && ./hack/e2e-uc1.sh

.PHONY: e2e-phase1-followups
e2e-phase1-followups:
	chmod +x hack/e2e-phase1-followups.sh && ./hack/e2e-phase1-followups.sh

.PHONY: e2e-nydus
e2e-nydus:
	chmod +x hack/e2e-nydus.sh && ./hack/e2e-nydus.sh

.PHONY: e2e-phase1-signoff
e2e-phase1-signoff:
	chmod +x hack/e2e-phase1-signoff.sh && ./hack/e2e-phase1-signoff.sh

.PHONY: docker-build
docker-build:
	docker build -t $(REGISTRY)/coda-controller-manager:latest -f hack/Dockerfile.controller .

.PHONY: docker-push-controller
docker-push-controller: docker-build
	docker push $(REGISTRY)/coda-controller-manager:latest

.PHONY: docker-build-gateway
docker-build-gateway:
	docker build -t $(REGISTRY)/coda-gateway:latest -f hack/Dockerfile.gateway .

.PHONY: docker-build-studio
docker-build-studio:
	docker build -t $(REGISTRY)/coda-studio:latest -f hack/Dockerfile.studio .

.PHONY: docker-push-gateway
docker-push-gateway: docker-build-gateway
	docker push $(REGISTRY)/coda-gateway:latest

.PHONY: docker-push-studio
docker-push-studio: docker-build-studio
	docker push $(REGISTRY)/coda-studio:latest

.PHONY: helm-package
helm-package:
	helm package charts/opencoda -d dist/

.PHONY: fmt
fmt:
	go fmt ./...

.PHONY: vet
vet:
	go vet ./...

define go-install-tool
@[ -f "$(1)-$(3)" ] || { \
set -e; \
echo "Downloading $(2)@$(3)" ; \
GOBIN=$(LOCALBIN) go install $(2)@$(3) ; \
mv $(LOCALBIN)/`basename $(2)` $(1)-$(3) ; \
} ; \
ln -sf $(1)-$(3) $(1)
endef
