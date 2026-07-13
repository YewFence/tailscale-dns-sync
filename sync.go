package main

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
)

type syncConfig struct {
	tailscaleAPIKey  string
	tailscaleTailnet string
	domainSuffix     string
	technitiumURL    string
	technitiumToken  string
	dnsTTL           int
}

func desiredARecords(devices map[string]string, suffix string) map[string]string {
	desired := make(map[string]string, len(devices)*2)
	for deviceName, ip := range devices {
		fqdn := normalizeDomain(deviceName + "." + suffix)
		desired[fqdn] = ip
		desired["*."+fqdn] = ip
	}

	return desired
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	return keys
}

func runSync(cfg syncConfig) error {
	log.Println("Starting Tailscale -> Technitium DNS sync...")

	technitium := newTechnitiumClient(cfg.technitiumURL, cfg.technitiumToken)

	var (
		devices map[string]string
		records []technitiumRecord
		devErr  error
		dnsErr  error
		wg      sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		devices, devErr = fetchTailscaleDevices(cfg.tailscaleAPIKey, cfg.tailscaleTailnet)
	}()
	go func() {
		defer wg.Done()
		records, dnsErr = technitium.ensureForwarderZone(cfg.domainSuffix)
	}()
	wg.Wait()

	if devErr != nil {
		return fmt.Errorf("fetch Tailscale devices: %w", devErr)
	}
	if dnsErr != nil {
		return fmt.Errorf("prepare Technitium zone: %w", dnsErr)
	}

	desired := desiredARecords(devices, cfg.domainSuffix)
	managed := make(map[string]technitiumRecord)
	unmanaged := make(map[string][]technitiumRecord)
	for _, record := range records {
		if !strings.EqualFold(record.Type, "A") {
			continue
		}

		name := normalizeDomain(record.Name)
		if record.Comments == managedRecordComment {
			if _, exists := managed[name]; exists {
				return fmt.Errorf("multiple managed A records found for %s", name)
			}
			managed[name] = record
		} else {
			unmanaged[name] = append(unmanaged[name], record)
		}
	}

	log.Printf("Tailscale devices: %d, desired records: %d, managed records: %d", len(devices), len(desired), len(managed))

	var added, updated, deleted int
	for _, domain := range sortedKeys(desired) {
		ip := desired[domain]
		if len(unmanaged[domain]) > 0 {
			return fmt.Errorf("refusing to modify %s because unmanaged A records already exist", domain)
		}

		existing, ok := managed[domain]
		if !ok {
			if err := technitium.addARecord(cfg.domainSuffix, domain, ip, cfg.dnsTTL); err != nil {
				return fmt.Errorf("add %s: %w", domain, err)
			}
			log.Printf("Added: %s -> %s", domain, ip)
			added++
			continue
		}

		delete(managed, domain)
		if existing.RData.IPAddress == "" {
			return fmt.Errorf("managed A record %s has no IP address", domain)
		}
		if existing.RData.IPAddress != ip || existing.TTL != cfg.dnsTTL || existing.Disabled {
			if err := technitium.updateARecord(cfg.domainSuffix, domain, existing.RData.IPAddress, ip, cfg.dnsTTL); err != nil {
				return fmt.Errorf("update %s: %w", domain, err)
			}
			log.Printf("Updated: %s %s -> %s", domain, existing.RData.IPAddress, ip)
			updated++
		} else {
			log.Printf("Unchanged: %s -> %s", domain, ip)
		}
	}

	for _, domain := range sortedKeys(managed) {
		record := managed[domain]
		if record.RData.IPAddress == "" {
			return fmt.Errorf("managed A record %s has no IP address", domain)
		}
		if err := technitium.deleteARecord(cfg.domainSuffix, domain, record.RData.IPAddress); err != nil {
			return fmt.Errorf("delete %s: %w", domain, err)
		}
		log.Printf("Deleted: %s", domain)
		deleted++
	}

	log.Printf("Sync complete. added=%d updated=%d deleted=%d", added, updated, deleted)
	return nil
}

func runPurge(cfg syncConfig) error {
	log.Println("Purging all managed Technitium DNS records...")

	technitium := newTechnitiumClient(cfg.technitiumURL, cfg.technitiumToken)
	zones, err := technitium.listZones()
	if err != nil {
		return err
	}

	zoneExists := false
	for _, zone := range zones {
		if strings.EqualFold(normalizeDomain(zone.Name), cfg.domainSuffix) {
			zoneExists = true
			break
		}
	}
	if !zoneExists {
		log.Println("Nothing to purge: zone does not exist.")
		return nil
	}

	records, err := technitium.fetchRecords(cfg.domainSuffix)
	if err != nil {
		return err
	}

	managed := make(map[string]technitiumRecord)
	for _, record := range records {
		if strings.EqualFold(record.Type, "A") && record.Comments == managedRecordComment {
			name := normalizeDomain(record.Name)
			if _, exists := managed[name]; exists {
				return fmt.Errorf("multiple managed A records found for %s", name)
			}
			managed[name] = record
		}
	}

	if len(managed) == 0 {
		log.Println("Nothing to purge.")
		return nil
	}

	for _, domain := range sortedKeys(managed) {
		record := managed[domain]
		if record.RData.IPAddress == "" {
			return fmt.Errorf("managed A record %s has no IP address", domain)
		}
		if err := technitium.deleteARecord(cfg.domainSuffix, domain, record.RData.IPAddress); err != nil {
			return fmt.Errorf("delete %s: %w", domain, err)
		}
		log.Printf("Deleted: %s", domain)
	}

	log.Printf("Purge complete. deleted=%d", len(managed))
	return nil
}
