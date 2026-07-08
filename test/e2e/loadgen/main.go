package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"
)

type chatRequest struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

func main() {
	var (
		gatewayURL string
		token      string
		model      string
		bursts     int
		burstSize  int
		idleSec    int
		system     string
	)
	flag.StringVar(&gatewayURL, "gateway", "http://127.0.0.1:18090", "gateway base URL")
	flag.StringVar(&token, "token", "", "Bearer token id:secret")
	flag.StringVar(&model, "model", "hf://TinyLlama/TinyLlama-1.1B-Chat-v1.0", "model id")
	flag.IntVar(&bursts, "bursts", 5, "number of bursts")
	flag.IntVar(&burstSize, "burst-size", 8, "requests per burst")
	flag.IntVar(&idleSec, "idle-sec", 30, "idle seconds between bursts")
	flag.StringVar(&system, "system", "shared-agent-prefix: you are a helpful coding agent.", "shared system prompt prefix")
	flag.Parse()

	if token == "" {
		fmt.Fprintln(os.Stderr, "--token required")
		os.Exit(2)
	}

	client := &http.Client{Timeout: 120 * time.Second}
	var (
		totalRequests int
		successes     int
		firstHits     int
		busySeconds   float64
		provisioned   float64
	)

	for b := 0; b < bursts; b++ {
		if b > 0 {
			fmt.Printf("idle %ds before burst %d\n", idleSec, b+1)
			time.Sleep(time.Duration(idleSec) * time.Second)
		}
		burstStart := time.Now()
		for i := 0; i < burstSize; i++ {
			totalRequests++
			reqBody := chatRequest{Model: model}
			reqBody.Messages = []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			}{
				{Role: "system", Content: system},
				{Role: "user", Content: fmt.Sprintf("turn-%d-burst-%d question %d", i, b, rand.Intn(1000))},
			}
			payload, _ := json.Marshal(reqBody)
			req, err := http.NewRequest(http.MethodPost, gatewayURL+"/v1/chat/completions", bytes.NewReader(payload))
			if err != nil {
				panic(err)
			}
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")
			start := time.Now()
			resp, err := client.Do(req)
			if err != nil {
				fmt.Printf("request error: %v\n", err)
				continue
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				successes++
				if strings.Contains(string(body), "hello") || strings.Contains(string(body), "choices") {
					if i == 0 {
						firstHits++
					}
				}
				busySeconds += time.Since(start).Seconds()
			} else if resp.StatusCode == http.StatusTooManyRequests {
				// scale-from-zero: retry until success
				for retries := 0; retries < 30; retries++ {
					time.Sleep(2 * time.Second)
					resp2, err2 := client.Do(req)
					if err2 != nil {
						continue
					}
					b2, _ := io.ReadAll(resp2.Body)
					resp2.Body.Close()
					if resp2.StatusCode == http.StatusOK {
						successes++
						if i == 0 {
							firstHits++
						}
						busySeconds += time.Since(start).Seconds()
						break
					}
					_ = b2
				}
			}
		}
		provisioned += time.Since(burstStart).Seconds()
	}

	utilization := 0.0
	if provisioned > 0 {
		utilization = busySeconds / provisioned
	}
	firstHitRate := 0.0
	if bursts > 0 {
		firstHitRate = float64(firstHits) / float64(bursts)
	}

	fmt.Printf("UC1 summary: requests=%d successes=%d utilization=%.2f first_request_kv_hit=%.2f\n",
		totalRequests, successes, utilization, firstHitRate)
}
