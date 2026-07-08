package constants

const (
	AnnotationDesiredReplicas = "opencoda.dev/desired-replicas"
	AnnotationSpecHash        = "opencoda.dev/spec-hash"
	AnnotationEndpoint        = "opencoda.dev/endpoint"
	AnnotationXidCritical     = "opencoda.dev/xid-critical"
	AnnotationDemandEWMA      = "opencoda.dev/demand-ewma"
	AnnotationPodCreatedAt    = "opencoda.dev/pod-created-at"
	AnnotationColdStartRecorded = "opencoda.dev/cold-start-recorded"
	LabelGPU                  = "opencoda.dev/gpu"
	LabelBufferEligible       = "opencoda.dev/buffer-eligible"
	LabelProvider             = "opencoda.dev/provider"
	LabelPool                 = "opencoda.dev/pool"
	LabelEndpoint             = "opencoda.dev/endpoint"
)
