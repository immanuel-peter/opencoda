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
	ClusterName  string
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
		cluster := cfg.ClusterName
		if cluster == "" {
			cluster = "opencoda"
		}
		joinCmd = fmt.Sprintf(
			"/etc/eks/bootstrap.sh %q --kubelet-extra-args '--node-labels=opencoda.dev/gpu=true,opencoda.dev/pool=%s,opencoda.dev/buffer-eligible=true'",
			cluster, cfg.PoolName)
	case "gke":
		joinCmd = "gcloud container clusters get-credentials ${CLUSTER_NAME:-opencoda} --zone ${ZONE:-us-central1-a}"
	default:
		joinCmd = fmt.Sprintf("kubeadm join %s --token %s --discovery-token-unsafe-skip-cause-unknown",
			cfg.APIServerURL, cfg.JoinToken)
	}
	return fmt.Sprintf(`#!/bin/bash
set -euo pipefail
# node labels for OpenCoda (pool=%s)
%s
`, cfg.PoolName, joinCmd)
}

// StartupScript is GCP metadata startup-script (no cloud-init wrapper).
func StartupScript(cfg Config) string {
	return strings.TrimSpace(UserData(cfg))
}
