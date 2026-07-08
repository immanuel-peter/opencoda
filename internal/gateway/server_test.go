package gateway

import (
	"encoding/json"
	"testing"
)

func TestServeModelID(t *testing.T) {
	if got := serveModelID("hf://Qwen/Qwen2.5-0.5B-Instruct"); got != "Qwen/Qwen2.5-0.5B-Instruct" {
		t.Fatalf("serveModelID=%q", got)
	}
}

func TestRewriteProxyModelBody(t *testing.T) {
	body := []byte(`{"model":"hf://Qwen/Qwen2.5-0.5B-Instruct","messages":[{"role":"user","content":"hi"}]}`)
	out := rewriteProxyModelBody(body, serveModelID("hf://Qwen/Qwen2.5-0.5B-Instruct"))
	var payload struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Model != "Qwen/Qwen2.5-0.5B-Instruct" {
		t.Fatalf("model=%q", payload.Model)
	}
}
