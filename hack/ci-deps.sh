#!/usr/bin/env bash
# Installs toolchain deps shared by GHA and Crabbox hydration.
set -euo pipefail

install_kind() {
  if command -v kind >/dev/null 2>&1; then
    return
  fi
  local ver="${KIND_VERSION:-v0.27.0}"
  curl -fsSL "https://kind.sigs.k8s.io/dl/${ver}/kind-$(uname -s | tr '[:upper:]' '[:lower:]')-$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')" -o /tmp/kind
  chmod +x /tmp/kind
  sudo mv /tmp/kind /usr/local/bin/kind
}

install_kubectl() {
  if command -v kubectl >/dev/null 2>&1; then
    return
  fi
  local ver="${KUBECTL_VERSION:-v1.32.0}"
  curl -fsSL "https://dl.k8s.io/release/${ver}/bin/linux/amd64/kubectl" -o /tmp/kubectl
  chmod +x /tmp/kubectl
  sudo mv /tmp/kubectl /usr/local/bin/kubectl
}

install_helm() {
  if command -v helm >/dev/null 2>&1; then
    return
  fi
  curl -fsSL https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash
}

install_awscli() {
  if command -v aws >/dev/null 2>&1; then
    return
  fi
  if command -v apt-get >/dev/null 2>&1; then
    sudo apt-get update -qq
    sudo apt-get install -y -qq awscli
  else
    echo "install aws CLI manually on this host" >&2
    exit 1
  fi
}

case "${1:-all}" in
  kind) install_kind ;;
  kubectl) install_kubectl ;;
  helm) install_helm ;;
  aws) install_awscli ;;
  e2e)
    install_kind
    install_kubectl
    install_helm
    ;;
  eks)
    install_kubectl
    install_helm
    install_awscli
    ;;
  all)
    install_kind
    install_kubectl
    install_helm
    install_awscli
    ;;
  *)
    echo "usage: $0 [kind|kubectl|helm|aws|e2e|eks|all]" >&2
    exit 2
    ;;
esac
