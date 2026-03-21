package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/robfig/cron/v3"
)

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("Missing required environment variable: %s", key)
	}
	return v
}

func main() {
	cfg := syncConfig{
		tailscaleAPIKey:  mustEnv("TAILSCALE_API_KEY"),
		tailscaleTailnet: mustEnv("TAILSCALE_TAILNET"),
		domainSuffix:     mustEnv("DOMAIN_SUFFIX"),
		adguardURL:       mustEnv("ADGUARD_URL"),
		adguardUsername:  mustEnv("ADGUARD_USERNAME"),
		adguardPassword:  mustEnv("ADGUARD_PASSWORD"),
	}

	cronSchedule := os.Getenv("CRON_SCHEDULE")
	if cronSchedule == "" {
		cronSchedule = "0 * * * *"
	}
	triggerToken := os.Getenv("TRIGGER_TOKEN")
	port, _ := strconv.Atoi(os.Getenv("PORT"))
	if port == 0 {
		port = 3001
	}

	// 启动时立即同步一次
	if err := runSync(cfg); err != nil {
		log.Printf("Initial sync failed: %v", err)
	}

	// 定时任务
	c := cron.New()
	if _, err := c.AddFunc(cronSchedule, func() {
		if err := runSync(cfg); err != nil {
			log.Printf("Scheduled sync failed: %v", err)
		}
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
		go func() {
			if err := runSync(cfg); err != nil {
				log.Printf("Manual sync failed: %v", err)
			}
		}()
	})

	mux.HandleFunc("POST /purge", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(w, r) {
			return
		}
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte("Purge triggered"))
		go func() {
			if err := runPurge(cfg); err != nil {
				log.Printf("Purge failed: %v", err)
			}
		}()
	})

	addr := fmt.Sprintf(":%d", port)
	log.Printf("HTTP server listening on port %d", port)
	log.Fatal(http.ListenAndServe(addr, mux))
}
