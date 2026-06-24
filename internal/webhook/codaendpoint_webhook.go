package webhook

import (
	"context"
	"fmt"
	"net/http"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	opencodav1alpha1 "github.com/immanuel-peter/opencoda/api/v1alpha1"
)

// CodaEndpointValidator validates CodaEndpoint resources.
type CodaEndpointValidator struct {
	decoder admission.Decoder
}

func NewCodaEndpointValidator(scheme *runtime.Scheme) *CodaEndpointValidator {
	return &CodaEndpointValidator{decoder: admission.NewDecoder(scheme)}
}

func (v *CodaEndpointValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	var ep opencodav1alpha1.CodaEndpoint
	if err := v.decoder.Decode(req, &ep); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	runtimeClass := ep.Spec.Runtime.Class
	if runtimeClass == "" {
		runtimeClass = "runc"
	}
	if ep.Spec.Snapshot != nil && runtimeClass != "runc" {
		return admission.Denied(fmt.Sprintf("SnapshotClass requires runtime.class=runc, got %q", runtimeClass))
	}

	if ep.Spec.Engine.Type != "" && ep.Spec.Engine.Type != "vllm" {
		return admission.Denied(fmt.Sprintf("engine type %q not supported in v1 (only vllm)", ep.Spec.Engine.Type))
	}

	return admission.Allowed("ok")
}

// RegisterCodaEndpointValidator registers the admission webhook on the server.
func RegisterCodaEndpointValidator(server webhook.Server, scheme *runtime.Scheme) {
	validator := NewCodaEndpointValidator(scheme)
	server.Register("/validate-opencoda-dev-v1alpha1-codaendpoint", &webhook.Admission{Handler: validator})
}
