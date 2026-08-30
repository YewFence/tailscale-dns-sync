package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/robfig/cron/v3"
	"golang.org/x/oauth2/clientcredentials"
)

// newTailscaleClient 构造 Tailscale API 客户端。优先使用 OAuth client（最小权限），
// 未配置时回退到全权限 API key 并打印迁移警告。
func newTailscaleClient() *http.Client {
	clientID := strings.TrimSpace(os.Getenv("TAILSCALE_OAUTH_CLIENT_ID"))
	clientSecret := strings.TrimSpace(os.Getenv("TAILSCALE_OAUTH_CLIENT_SECRET"))
	apiKey := strings.TrimSpace(os.Getenv("TAILSCALE_API_KEY"))

	switch {
	case clientID != "" && clientSecret != "":
		log.Println("Tailscale auth: OAuth client (scoped)")
		oauth := &clientcredentials.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			TokenURL:     "https://api.tailscale.com/api/v2/oauth/token",
		}
		return oauth.Client(context.Background())
	case clientID != "" || clientSecret != "":
		log.Fatal("TAILSCALE_OAUTH_CLIENT_ID and TAILSCALE_OAUTH_CLIENT_SECRET must be set together")
	case apiKey != "":
		log.Println("WARNING: TAILSCALE_API_KEY grants FULL access to the tailnet; migrate to a scoped OAuth client: https://tailscale.com/docs/reference/trust-credentials")
		return &http.Client{Transport: bearerTransport{base: http.DefaultTransport, token: apiKey}}
	default:
		log.Fatal("Set TAILSCALE_OAUTH_CLIENT_ID and TAILSCALE_OAUTH_CLIENT_SECRET, or TAILSCALE_API_KEY (deprecated)")
	}
	return nil
}

// bearerTransport 为每个请求注入 Bearer token（仅用于已弃用的 API key 模式）。
type bearerTransport struct {
	base  http.RoundTripper
	token string
}

func (t bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(req)
}

func mustEnv(key string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		log.Fatalf("Missing required environment variable: %s", key)
	}
	return v
}

func normalizeDomain(domain string) string {
	return strings.ToLower(strings.Trim(strings.TrimSpace(domain), "."))
}

func envInt(key string, defaultValue int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return defaultValue
	}

	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		log.Fatalf("Invalid positive integer for %s: %q", key, raw)
	}
	return value
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "init-technitium" {
		if err := runTechnitiumInit(); err != nil {
			log.Fatalf("Technitium initialization failed: %v", err)
		}
		return
	}

	domainSuffix := normalizeDomain(mustEnv("DOMAIN_SUFFIX"))
	if domainSuffix == "" {
		log.Fatal("DOMAIN_SUFFIX must contain a domain name")
	}

	cfg := syncConfig{
		tailscaleClient:  newTailscaleClient(),
		tailscaleTailnet: mustEnv("TAILSCALE_TAILNET"),
		domainSuffix:     domainSuffix,
		technitiumURL:    mustEnv("TECHNITIUM_URL"),
		technitiumToken:  mustEnvOrFile("TECHNITIUM_TOKEN"),
		dnsTTL:           envInt("DNS_TTL", 60),
	}

	cronSchedule := os.Getenv("CRON_SCHEDULE")
	if cronSchedule == "" {
		cronSchedule = "0 * * * *"
	}
	triggerToken := os.Getenv("TRIGGER_TOKEN")
	port := envInt("PORT", 3001)
	var operationMu sync.Mutex
	runOperation := func(name string, operation func() error) {
		if !operationMu.TryLock() {
			log.Printf("%s skipped: another DNS operation is already running", name)
			return
		}
		defer operationMu.Unlock()

		if err := operation(); err != nil {
			log.Printf("%s failed: %v", name, err)
		}
	}

	// 启动时立即同步一次
	runOperation("Initial sync", func() error { return runSync(cfg) })

	// 定时任务
	c := cron.New()
	if _, err := c.AddFunc(cronSchedule, func() {
		runOperation("Scheduled sync", func() error { return runSync(cfg) })
	}); err != nil {
		log.Fatalf("Invalid cron schedule %q: %v", cronSchedule, err)
	}
	c.Start()
	log.Printf("Cron scheduled: %s", cronSchedule)

	// 鉴权中间件
	checkAuth := func(w http.ResponseWriter, r *http.Request) bool {
		if triggerToken == "" {
			return true
		}
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token != triggerToken {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return false
		}
		return true
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	mux.HandleFunc("POST /trigger", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(w, r) {
			return
		}
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte("Sync triggered"))
		go runOperation("Manual sync", func() error { return runSync(cfg) })
	})

	mux.HandleFunc("POST /purge", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(w, r) {
			return
		}
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte("Purge triggered"))
		go runOperation("Purge", func() error { return runPurge(cfg) })
	})

	addr := fmt.Sprintf(":%d", port)
	log.Printf("HTTP server listening on port %d", port)
	log.Fatal(http.ListenAndServe(addr, mux))
}
