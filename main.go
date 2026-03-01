package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"github.com/logclaw/agent/collectors"
)

// MetricsPayload is the JSON body posted to LogClaw SaaS.
type MetricsPayload struct {
	TenantID    string                         `json:"tenantId"`
	CollectedAt string                         `json:"collectedAt"`
	KafkaLag    map[string]int64               `json:"kafkaLag"`
	FlinkJobs   []collectors.FlinkJob          `json:"flinkJobs"`
	OsHealth    collectors.OSHealth            `json:"osHealth"`
	ESOStatus   []collectors.ESOExternalSecret `json:"esoStatus"`
}

// ready is set to 1 after the first successful collection cycle.
var ready atomic.Int32

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required env var %s is not set", key)
	}
	return v
}

func push(ctx context.Context, endpoint, jwt string, payload MetricsPayload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+jwt)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}

func startHealthServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if ready.Load() == 1 {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintln(w, "ok")
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintln(w, "not ready")
		}
	})

	server := &http.Server{
		Addr:              ":8080",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("Health server listening on :8080")
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Printf("ERROR health server: %v", err)
	}
}

func main() {
	tenantID := mustEnv("LOGCLAW_TENANT_ID")
	saasURL := mustEnv("LOGCLAW_SAAS_URL") // e.g. https://app.logclaw.ai/api/metrics
	agentJWT := mustEnv("LOGCLAW_AGENT_JWT")
	namespace := os.Getenv("LOGCLAW_NAMESPACE")
	if namespace == "" {
		namespace = "default"
	}

	go startHealthServer()

	interval := 30 * time.Second
	maxBackoff := 5 * time.Minute
	backoff := interval

	log.Printf("LogClaw agent starting: tenant=%s namespace=%s", tenantID, namespace)

	for {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)

		kafkaLag, err := collectors.KafkaLag(ctx, namespace)
		if err != nil {
			log.Printf("WARN kafka collector: %v", err)
			kafkaLag = map[string]int64{}
		}

		flinkJobs, err := collectors.FlinkJobs(ctx, namespace)
		if err != nil {
			log.Printf("WARN flink collector: %v", err)
			flinkJobs = []collectors.FlinkJob{}
		}

		osHealth, err := collectors.OpenSearchHealth(ctx, namespace)
		if err != nil {
			log.Printf("WARN opensearch collector: %v", err)
			osHealth = collectors.OSHealth{Status: "unknown"}
		}

		esoStatus, err := collectors.ESOStatus(ctx, namespace)
		if err != nil {
			log.Printf("WARN eso collector: %v", err)
			esoStatus = []collectors.ESOExternalSecret{}
		}

		cancel()

		// Mark ready after first successful collection cycle
		ready.Store(1)

		payload := MetricsPayload{
			TenantID:    tenantID,
			CollectedAt: time.Now().UTC().Format(time.RFC3339),
			KafkaLag:    kafkaLag,
			FlinkJobs:   flinkJobs,
			OsHealth:    osHealth,
			ESOStatus:   esoStatus,
		}

		pushCtx, pushCancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := push(pushCtx, saasURL, agentJWT, payload); err != nil {
			log.Printf("ERROR push failed: %v — backing off %s", err, backoff)
			pushCancel()
			time.Sleep(backoff)
			backoff = time.Duration(math.Min(float64(backoff*2), float64(maxBackoff)))
			continue
		}
		pushCancel()

		log.Printf("Metrics pushed: kafka_topics=%d flink_jobs=%d eso_secrets=%d",
			len(kafkaLag), len(flinkJobs), len(esoStatus))

		backoff = interval // reset on success
		time.Sleep(interval)
	}
}
