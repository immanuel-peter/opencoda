package bootstrap

import (
	"fmt"
	"strings"
)

// Config is cluster join + node bootstrap material passed into userdata.
type Config struct {
	APIServerURL string
	CABundle     string
	JoinToken    string
	PoolName     string
	JoinMode     string // kubeadm | eks | gke
}

// UserData returns cloud-init script for GPU node bootstrap (§16 containerd + join).
func UserData(cfg Config) string {
	joinMode := cfg.JoinMode
	if joinMode == "" {
		joinMode = "kubeadm"
	}
	var joinCmd string
	switch joinMode {
	case "eks":
		joinCmd = "/etc/eks/bootstrap.sh ${CLUSTER_NAME:-opencoda}"
	case "gke":
		joinCmd = "gcloud container clusters get-credentials ${CLUSTER_NAME:-opencoda} --zone ${ZONE:-us-central1-a}"
	default:
		joinCmd = fmt.Sprintf("kubeadm join %s --token %s --discovery-token-unsafe-skip-cause-unknown",
			cfg.APIServerURL, cfg.JoinToken)
	}
	return fmt.Sprintf(`#!/bin/bash
set -euo pipefail
# containerd: lazy pull + Spegel mirror path (§16)
mkdir -p /etc/containerd/certs.d
cat >/etc/containerd/config.toml <<'EOF'
version = 2
[plugins."io.containerd.cri.v1.images"]
  discard_unpacked_layers = false
  use_local_image_pull = true
[plugins."io.containerd.cri.v1.images".registry]
  config_path = "/etc/containerd/certs.d"
[plugins."io.containerd.grpc.v1.cri".containerd]
  disable_snapshot_annotations = false
EOF
systemctl restart containerd || true
# node labels for OpenCoda
POOL=%s
echo "opencoda pool bootstrap for $POOL"
%s
`, cfg.PoolName, joinCmd)
}

// StartupScript is GCP metadata startup-script (no cloud-init wrapper).
func StartupScript(cfg Config) string {
	return strings.TrimSpace(UserData(cfg))
}
