package main

import (
	"fmt"
	"log"
	"strings"
	"sync"
)

type syncConfig struct {
	tailscaleAPIKey  string
	tailscaleTailnet string
	domainSuffix     string
	adguardURL       string
	adguardUsername  string
	adguardPassword  string
}

func runSync(cfg syncConfig) error {
	log.Println("Starting Tailscale → AdGuard Home DNS sync...")

	adguard := newAdGuardClient(cfg.adguardURL, cfg.adguardUsername, cfg.adguardPassword)

	// 并发拉取两端数据
	var (
		devices  map[string]string
		rewrites []rewriteEntry
		devErr   error
		rwErr    error
		wg       sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		devices, devErr = fetchTailscaleDevices(cfg.tailscaleAPIKey, cfg.tailscaleTailnet)
	}()
	go func() {
		defer wg.Done()
		rewrites, rwErr = adguard.fetchRewrites()
	}()
	wg.Wait()

	if devErr != nil {
		return fmt.Errorf("fetch tailscale devices: %w", devErr)
	}
	if rwErr != nil {
		return fmt.Errorf("fetch adguard rewrites: %w", rwErr)
	}

	suffix := "." + cfg.domainSuffix
	rewriteMap := make(map[string]string)
	for _, r := range rewrites {
		if strings.HasSuffix(r.Domain, suffix) {
			rewriteMap[r.Domain] = r.Answer
		}
	}

	log.Printf("Tailscale devices: %d, managed rewrites: %d", len(devices), len(rewriteMap))

	var added, updated, deleted int

	for deviceName, ip := range devices {
		fqdn := deviceName + suffix
		wildcard := "*." + deviceName + suffix
		for _, domain := range []string{fqdn, wildcard} {
			existing, ok := rewriteMap[domain]
			if !ok {
				if err := adguard.addRewrite(domain, ip); err != nil {
					return err
				}
				log.Printf("Added: %s → %s", domain, ip)
				added++
			} else if existing != ip {
				if err := adguard.updateRewrite(domain, existing, ip); err != nil {
					return err
				}
				log.Printf("Updated: %s %s → %s", domain, existing, ip)
				updated++
			} else {
				log.Printf("Unchanged: %s → %s", domain, ip)
			}
		}
	}

	for domain, ip := range rewriteMap {
		base := strings.TrimPrefix(domain, "*.")
		deviceName := strings.TrimSuffix(base, suffix)
		if _, exists := devices[deviceName]; !exists {
			if err := adguard.deleteRewrite(domain, ip); err != nil {
				return err
			}
			log.Printf("Deleted: %s", domain)
			deleted++
		}
	}

	log.Printf("Sync complete. added=%d updated=%d deleted=%d", added, updated, deleted)
	return nil
}

func runPurge(cfg syncConfig) error {
	log.Println("Purging all managed DNS rewrites...")

	adguard := newAdGuardClient(cfg.adguardURL, cfg.adguardUsername, cfg.adguardPassword)
	allRewrites, err := adguard.fetchRewrites()
	if err != nil {
		return err
	}

	suffix := "." + cfg.domainSuffix
	var managed []rewriteEntry
	for _, r := range allRewrites {
		if strings.HasSuffix(r.Domain, suffix) {
			managed = append(managed, r)
		}
	}

	if len(managed) == 0 {
		log.Println("Nothing to purge.")
		return nil
	}

	for _, r := range managed {
		if err := adguard.deleteRewrite(r.Domain, r.Answer); err != nil {
			return err
		}
		log.Printf("Deleted: %s", r.Domain)
	}
	log.Printf("Purge complete. deleted=%d", len(managed))
	return nil
}
