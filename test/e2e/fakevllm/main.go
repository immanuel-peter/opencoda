package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
)

var (
	prefixHits    uint64
	prefixQueries uint64
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		w.Write([]byte("# HELP coda_prefix_cache_hits_total Prefix cache hits\n"))
		w.Write([]byte("# TYPE coda_prefix_cache_hits_total counter\n"))
		w.Write([]byte("coda_prefix_cache_hits_total "))
		w.Write([]byte(itoa(atomic.LoadUint64(&prefixHits))))
		w.Write([]byte("\n"))
		w.Write([]byte("# HELP coda_prefix_cache_queries_total Prefix cache queries\n"))
		w.Write([]byte("# TYPE coda_prefix_cache_queries_total counter\n"))
		w.Write([]byte("coda_prefix_cache_queries_total "))
		w.Write([]byte(itoa(atomic.LoadUint64(&prefixQueries))))
		w.Write([]byte("\n"))
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.Unmarshal(body, &req)
		atomic.AddUint64(&prefixQueries, 1)
		prefix := ""
		if len(req.Messages) > 0 {
			prefix = req.Messages[0].Content
		}
		if strings.Contains(prefix, "shared-agent-prefix") {
			atomic.AddUint64(&prefixHits, 1)
		}
		resp := map[string]interface{}{
			"id":     "chatcmpl-fake",
			"object": "chat.completion",
			"choices": []map[string]interface{}{
				{
					"index": 0,
					"message": map[string]string{
						"role":    "assistant",
						"content": "hello from fakevllm",
					},
					"finish_reason": "stop",
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	})
	log.Println("fakevllm listening on :8000")
	log.Fatal(http.ListenAndServe(":8000", mux))
}

func itoa(v uint64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
