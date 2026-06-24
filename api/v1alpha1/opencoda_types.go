/*
Copyright 2026 OpenCoda contributors.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Condition types shared across resources.
const (
	ConditionTypeReady = "Ready"
)

// +kubebuilder:object:root=true

// GPUPoolList contains a list of GPUPool.
type GPUPoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GPUPool `json:"items"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Provider",type=string,JSONPath=`.spec.provider.name`
// +kubebuilder:printcolumn:name="Active",type=integer,JSONPath=`.status.nodes.active`
// +kubebuilder:printcolumn:name="Buffered",type=integer,JSONPath=`.status.nodes.buffered`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// GPUPool defines a homogeneous source of GPU capacity.
type GPUPool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GPUPoolSpec   `json:"spec,omitempty"`
	Status GPUPoolStatus `json:"status,omitempty"`
}

type GPUPoolSpec struct {
	Provider      ProviderSpec   `json:"provider"`
	InstanceTypes []string       `json:"instanceTypes,omitempty"`
	GPU           GPUProfile     `json:"gpu"`
	Limits        PoolLimits     `json:"limits,omitempty"`
	Priority      int            `json:"priority,omitempty"`
	Taints        []TaintSpec    `json:"taints,omitempty"`
}

type ProviderSpec struct {
	Name           string            `json:"name"`
	CredentialsRef SecretRef         `json:"credentialsRef,omitempty"`
	Params         map[string]string `json:"params,omitempty"`
}

type SecretRef struct {
	SecretName string `json:"secretName"`
}

type GPUProfile struct {
	Type    string `json:"type"`
	PerNode int    `json:"perNode"`
}

type PoolLimits struct {
	MaxNodes     int     `json:"maxNodes,omitempty"`
	MaxHourlyUSD float64 `json:"maxHourlyUSD,omitempty"`
}

type TaintSpec struct {
	Key    string `json:"key"`
	Effect string `json:"effect"`
}

type GPUPoolStatus struct {
	ObservedCapacity ObservedCapacity `json:"observedCapacity,omitempty"`
	Nodes            PoolNodeCounts   `json:"nodes,omitempty"`
	NodeRecords      []NodeRecord     `json:"nodeRecords,omitempty"`
	Conditions       []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// NodeRecord tracks a provisioned or joined node in a pool.
type NodeRecord struct {
	ProviderID string      `json:"providerID"`
	NodeName   string      `json:"nodeName,omitempty"`
	State      string      `json:"state"` // provisioning | buffered | active | draining | released
	LaunchedAt metav1.Time `json:"launchedAt,omitempty"`
	PoolName   string      `json:"poolName,omitempty"`
}

type ObservedCapacity struct {
	Available         int       `json:"available,omitempty"`
	LastICE           metav1.Time `json:"lastICE,omitempty"`
	ObservedHourlyUSD float64   `json:"observedHourlyUSD,omitempty"`
}

type PoolNodeCounts struct {
	Active        int `json:"active,omitempty"`
	Buffered      int `json:"buffered,omitempty"`
	Provisioning  int `json:"provisioning,omitempty"`
}

// +kubebuilder:object:root=true

// BufferPolicyList contains a list of BufferPolicy.
type BufferPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BufferPolicy `json:"items"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster

// BufferPolicy defines buffer sizing and pool ordering.
type BufferPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BufferPolicySpec   `json:"spec,omitempty"`
	Status BufferPolicyStatus `json:"status,omitempty"`
}

type BufferPolicySpec struct {
	Target    BufferTarget   `json:"target"`
	Pools     []PoolRef      `json:"pools"`
	ScaleDown ScaleDownSpec  `json:"scaleDown,omitempty"`
}

type BufferTarget struct {
	Mode          string         `json:"mode"` // static | dynamic
	MinWarmGPUs   int            `json:"minWarmGPUs,omitempty"`
	MaxWarmGPUs   int            `json:"maxWarmGPUs,omitempty"`
	Dynamic       *DynamicTarget `json:"dynamic,omitempty"`
}

type DynamicTarget struct {
	Window  string  `json:"window,omitempty"`
	Formula string  `json:"formula,omitempty"`
	K       float64 `json:"k,omitempty"`
}

type PoolRef struct {
	Name string `json:"name"`
}

type ScaleDownSpec struct {
	StabilizationWindow string `json:"stabilizationWindow,omitempty"`
	DrainTimeout        string `json:"drainTimeout,omitempty"`
}

type BufferPolicyStatus struct {
	CurrentWarmGPUs int                `json:"currentWarmGPUs,omitempty"`
	DemandEWMA      float64            `json:"demandEWMA,omitempty"`
	Conditions      []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +kubebuilder:object:root=true

// CodaEndpointList contains a list of CodaEndpoint.
type CodaEndpointList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CodaEndpoint `json:"items"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=integer,JSONPath=`.status.replicas.ready`
// +kubebuilder:printcolumn:name="Starting",type=integer,JSONPath=`.status.replicas.starting`
// +kubebuilder:printcolumn:name="KV Hit",type=string,JSONPath=`.status.kvHitRate`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// CodaEndpoint defines a model serving workload.
type CodaEndpoint struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CodaEndpointSpec   `json:"spec,omitempty"`
	Status CodaEndpointStatus `json:"status,omitempty"`
}

type CodaEndpointSpec struct {
	Model     ModelSpec     `json:"model"`
	Engine    EngineSpec    `json:"engine"`
	Resources ResourceSpec  `json:"resources"`
	Runtime   RuntimeSpec   `json:"runtime,omitempty"`
	Scaling   ScalingSpec   `json:"scaling"`
	Snapshot  *SnapshotRef  `json:"snapshot,omitempty"`
	KV        KVSpec        `json:"kv,omitempty"`
}

type ModelSpec struct {
	Source       string `json:"source"`
	Quantization string `json:"quantization,omitempty"`
}

type EngineSpec struct {
	Type    string   `json:"type"` // vllm in v1
	Version string   `json:"version,omitempty"`
	Args    []string `json:"args,omitempty"`
}

type ResourceSpec struct {
	GPU     int    `json:"gpu"`
	GPUType string `json:"gpuType,omitempty"`
}

type RuntimeSpec struct {
	Class string `json:"class,omitempty"` // runc | gvisor
}

type ScalingSpec struct {
	MinReplicas        int           `json:"minReplicas"`
	MaxReplicas        int           `json:"maxReplicas"`
	Target             ScalingTarget `json:"target"`
	ScaleToZeroAfter   string        `json:"scaleToZeroAfter,omitempty"`
}

type ScalingTarget struct {
	Metric string `json:"metric"`
	Value  int    `json:"value"`
}

type SnapshotRef struct {
	ClassRef string `json:"classRef"`
}

type KVSpec struct {
	LMCache *LMCacheSpec `json:"lmcache,omitempty"`
}

type LMCacheSpec struct {
	Enabled   bool     `json:"enabled,omitempty"`
	Shared    bool     `json:"shared,omitempty"`
	Tiers     []string `json:"tiers,omitempty"`
	RemoteRef RemoteRef `json:"remoteRef,omitempty"`
}

type RemoteRef struct {
	Bucket string `json:"bucket,omitempty"`
	Prefix string `json:"prefix,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
}

type CodaEndpointStatus struct {
	Replicas  ReplicaStatus    `json:"replicas,omitempty"`
	ColdStart ColdStartMetrics `json:"coldStart,omitempty"`
	KVHitRate float64          `json:"kvHitRate,omitempty"`
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

type ReplicaStatus struct {
	Ready    int `json:"ready,omitempty"`
	Starting int `json:"starting,omitempty"`
}

type ColdStartMetrics struct {
	P50Ms int64 `json:"p50ms,omitempty"`
	P95Ms int64 `json:"p95ms,omitempty"`
}

// +kubebuilder:object:root=true

// SnapshotClassList contains a list of SnapshotClass.
type SnapshotClassList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SnapshotClass `json:"items"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster

// SnapshotClass defines snapshot behavior (Phase 2+ restore path).
type SnapshotClass struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec SnapshotClassSpec `json:"spec,omitempty"`
}

type SnapshotClassSpec struct {
	Scope            string          `json:"scope,omitempty"`
	CheckpointAfter  string          `json:"checkpointAfter,omitempty"`
	Pre              SnapshotPreSpec `json:"pre,omitempty"`
	Storage          StorageRef      `json:"storage,omitempty"`
	Retention        RetentionSpec   `json:"retention,omitempty"`
}

type SnapshotPreSpec struct {
	OffloadWeightsToHost bool `json:"offloadWeightsToHost,omitempty"`
	DropKVCache          bool `json:"dropKVCache,omitempty"`
}

type StorageRef struct {
	Ref         string `json:"ref,omitempty"`
	Compression string `json:"compression,omitempty"`
}

type RetentionSpec struct {
	MaxPerKey int    `json:"maxPerKey,omitempty"`
	TTL       string `json:"ttl,omitempty"`
}

// +kubebuilder:object:root=true

// CodaTokenList contains a list of CodaToken.
type CodaTokenList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CodaToken `json:"items"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// CodaToken is a gateway auth credential.
type CodaToken struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CodaTokenSpec   `json:"spec,omitempty"`
	Status CodaTokenStatus `json:"status,omitempty"`
}

type CodaTokenSpec struct {
	TokenID    string `json:"tokenID"`
	SecretHash string `json:"secretHash,omitempty"`
	ExpiresAt  *metav1.Time `json:"expiresAt,omitempty"`
}

type CodaTokenStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +kubebuilder:object:root=true

// CodaFunctionList contains a list of CodaFunction.
type CodaFunctionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CodaFunction `json:"items"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// CodaFunction defines a batch/job workload.
type CodaFunction struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CodaFunctionSpec   `json:"spec,omitempty"`
	Status CodaFunctionStatus `json:"status,omitempty"`
}

type CodaFunctionSpec struct {
	Image     string            `json:"image"`
	Resources ResourceSpec      `json:"resources,omitempty"`
	Timeout   string            `json:"timeout,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
}

type CodaFunctionStatus struct {
	Phase      string             `json:"phase,omitempty"`
	StartTime  *metav1.Time       `json:"startTime,omitempty"`
	FinishTime *metav1.Time       `json:"finishTime,omitempty"`
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

func init() {
	SchemeBuilder.Register(
		&GPUPool{}, &GPUPoolList{},
		&BufferPolicy{}, &BufferPolicyList{},
		&CodaEndpoint{}, &CodaEndpointList{},
		&SnapshotClass{}, &SnapshotClassList{},
		&CodaToken{}, &CodaTokenList{},
		&CodaFunction{}, &CodaFunctionList{},
	)
}
