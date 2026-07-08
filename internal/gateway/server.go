package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	opencodav1alpha1 "github.com/immanuel-peter/opencoda/api/v1alpha1"
)

// Router dispatches requests among live replicas (thin proxy v1).
type Router struct {
	mu        sync.RWMutex
	endpoints map[string]*EndpointState
}

type EndpointState struct {
	Name           string
	ModelID        string
	ReplicaURLs    []string
	InFlight       int
	QueueDepth     int
	DesiredReplicas int
}

func NewRouter() *Router {
	return &Router{endpoints: make(map[string]*EndpointState)}
}

func (r *Router) RegisterEndpoint(name, modelID string, urls []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.endpoints[name] = &EndpointState{Name: name, ModelID: modelID, ReplicaURLs: urls}
}

func (r *Router) RegisterByModel(modelID string, ep *EndpointState) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.endpoints[ep.Name] = ep
}

func (r *Router) PickReplica(endpointName string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	ep, ok := r.endpoints[endpointName]
	if !ok || len(ep.ReplicaURLs) == 0 {
		return ""
	}
	idx := ep.InFlight % len(ep.ReplicaURLs)
	ep.InFlight++
	return ep.ReplicaURLs[idx]
}

func (r *Router) InFlight(endpointName string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if ep, ok := r.endpoints[endpointName]; ok {
		return ep.InFlight
	}
	return 0
}

func (r *Router) QueueDepth(endpointName string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if ep, ok := r.endpoints[endpointName]; ok {
		return ep.QueueDepth
	}
	return 0
}

func (r *Router) IncQueue(endpointName string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ep, ok := r.endpoints[endpointName]; ok {
		ep.QueueDepth++
	}
}

func (r *Router) DecQueue(endpointName string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ep, ok := r.endpoints[endpointName]; ok && ep.QueueDepth > 0 {
		ep.QueueDepth--
	}
}

func (r *Router) PickEndpointByModel(model string) *EndpointState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, ep := range r.endpoints {
		if ep.ModelID == model || ep.Name == model {
			return ep
		}
	}
	return nil
}

// Autoscaler adjusts desired replicas per endpoint.
type Autoscaler struct {
	router *Router
	ewma   map[string]float64
}

func NewAutoscaler(router *Router) *Autoscaler {
	return &Autoscaler{router: router, ewma: make(map[string]float64)}
}

func (a *Autoscaler) Evaluate(ep *opencodav1alpha1.CodaEndpoint, inFlight, queueDepth int) int {
	min := ep.Spec.Scaling.MinReplicas
	max := ep.Spec.Scaling.MaxReplicas
	target := ep.Spec.Scaling.Target.Value
	if target <= 0 {
		target = 8
	}

	desired := (inFlight + queueDepth) / target
	if desired < min {
		desired = min
	}
	if desired > max {
		desired = max
	}

	// EWMA demand signal for buffer policy
	alpha := 0.3
	prev := a.ewma[ep.Name]
	a.ewma[ep.Name] = alpha*float64(inFlight+queueDepth) + (1-alpha)*prev

	return desired
}

func (a *Autoscaler) DemandForecast(endpoint string) float64 {
	return a.ewma[endpoint]
}

func (a *Autoscaler) TotalDemand() float64 {
	var total float64
	for _, v := range a.ewma {
		total += v
	}
	return total
}

// Server is the HTTP gateway.
type Server struct {
	router     *Router
	autoscaler *Autoscaler
	auth       *TokenAuth
	addr       string
	kvSync     *K8sSync
}

func NewServer(addr string, auth *TokenAuth) *Server {
	return &Server{
		autoscaler: NewAutoscaler(NewRouter()),
		auth:       auth,
		addr:       addr,
	}
}

func (s *Server) SetRouter(r *Router) {
	s.router = r
	s.autoscaler = NewAutoscaler(r)
}

func (s *Server) SetK8sSync(sync *K8sSync) {
	s.kvSync = sync
}

func (s *Server) ListenAndServe() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("/v1/models", s.handleModels)
	mux.HandleFunc("/v1/chat/completions", s.handleChatCompletions)
	mux.HandleFunc("/metrics", s.handleMetrics)
	mux.HandleFunc("/v1/studio/endpoints", s.handleStudioEndpoints)
	mux.HandleFunc("/v1/studio/logs/stream", s.handleStudioLogs)
	return http.ListenAndServe(s.addr, s.auth.Middleware(mux))
}

func (s *Server) handleStudioEndpoints(w http.ResponseWriter, r *http.Request) {
	type ep struct {
		Name      string  `json:"name"`
		Ready     int     `json:"ready"`
		Starting  int     `json:"starting"`
		KVHitRate float64 `json:"kvHitRate"`
		Model     string  `json:"model"`
	}
	var out []ep
	s.router.mu.RLock()
	for _, e := range s.router.endpoints {
		kvRate := 0.0
		if s.kvSync != nil {
			kvRate = s.kvSync.KVHitRate(e.Name)
		}
		out = append(out, ep{Name: e.Name, Ready: len(e.ReplicaURLs), Starting: 0, KVHitRate: kvRate, Model: e.ModelID})
	}
	s.router.mu.RUnlock()
	json.NewEncoder(w).Encode(out)
}

func (s *Server) handleStudioLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, ok := w.(http.Flusher)
	for i := 0; i < 3; i++ {
		fmt.Fprintf(w, "[studio] log line %d\n", i)
		if ok {
			flusher.Flush()
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	type model struct {
		ID string `json:"id"`
	}
	var models []model
	s.router.mu.RLock()
	for _, ep := range s.router.endpoints {
		models = append(models, model{ID: ep.ModelID})
	}
	s.router.mu.RUnlock()
	json.NewEncoder(w).Encode(map[string]interface{}{"data": models})
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req struct {
		Model string `json:"model"`
	}
	json.Unmarshal(body, &req)

	ep := s.router.PickEndpointByModel(req.Model)
	if ep == nil {
		http.Error(w, "model not found", http.StatusNotFound)
		return
	}

	s.router.IncQueue(ep.Name)
	defer s.router.DecQueue(ep.Name)

	url := s.router.PickReplica(ep.Name)
	if url == "" {
		// queue during 0->1
		w.Header().Set("Retry-After", "5")
		http.Error(w, "no replicas ready", http.StatusTooManyRequests)
		return
	}

	proxyBody := rewriteProxyModelBody(body, serveModelID(req.Model))
	proxyReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, url+"/v1/chat/completions", bytes.NewReader(proxyBody))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	proxyReq.Header = r.Header.Clone()
	resp, err := http.DefaultClient.Do(proxyReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// TokenAuth validates CodaToken bearer tokens.
type TokenAuth struct {
	validator *TokenValidator
}

func NewTokenAuth(v *TokenValidator) *TokenAuth {
	return &TokenAuth{validator: v}
}

func (t *TokenAuth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		auth := r.Header.Get("Authorization")
		if auth == "" || t.validator == nil || !t.validator.Validate(r.Context(), auth) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("# gateway metrics placeholder\n"))
}

// serveModelID strips the hf:// prefix CodaEndpoint uses for routing.
func serveModelID(model string) string {
	return strings.TrimPrefix(model, "hf://")
}

func rewriteProxyModelBody(body []byte, servedModel string) []byte {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}
	encoded, err := json.Marshal(servedModel)
	if err != nil {
		return body
	}
	payload["model"] = encoded
	out, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return out
}
