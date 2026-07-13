package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultTechnitiumTokenName   = "tailscale-dns-sync"
	defaultTechnitiumInitTimeout = 2 * time.Minute
	technitiumInitRetryInterval  = 2 * time.Second
)

func envOrDefault(key, defaultValue string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue
	}
	return value
}

func readEnvOrFile(key string) (string, error) {
	value := strings.TrimSpace(os.Getenv(key))
	filePath := strings.TrimSpace(os.Getenv(key + "_FILE"))
	if value != "" && filePath != "" {
		return "", fmt.Errorf("%s and %s_FILE cannot both be set", key, key)
	}
	if value != "" {
		return value, nil
	}
	if filePath == "" {
		return "", fmt.Errorf("missing required environment variable: %s or %s_FILE", key, key)
	}

	contents, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read %s_FILE %s: %w", key, filePath, err)
	}
	value = strings.TrimSpace(string(contents))
	if value == "" {
		return "", fmt.Errorf("%s_FILE %s is empty", key, filePath)
	}
	return value, nil
}

func mustEnvOrFile(key string) string {
	value, err := readEnvOrFile(key)
	if err != nil {
		log.Fatal(err)
	}
	return value
}

func writeSecretFile(path, value string) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create secret directory %s: %w", directory, err)
	}

	temporary, err := os.CreateTemp(directory, ".technitium-token-*")
	if err != nil {
		return fmt.Errorf("create temporary token file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("set token file permissions: %w", err)
	}
	if _, err := temporary.WriteString(value + "\n"); err != nil {
		temporary.Close()
		return fmt.Errorf("write token file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close token file: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("install token file: %w", err)
	}

	return nil
}

func ensureTechnitiumToken(client *technitiumClient, username, password, tokenName, tokenFile string) error {
	if contents, err := os.ReadFile(tokenFile); err == nil {
		storedToken := strings.TrimSpace(string(contents))
		if storedToken != "" {
			client.token = storedToken
			if _, err := client.listZones(); err == nil {
				log.Println("Existing Technitium API token is valid; reusing it.")
				return nil
			}
			log.Println("Existing Technitium API token is no longer valid; replacing it.")
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read existing token file %s: %w", tokenFile, err)
	}

	client.token = ""
	token, err := client.createAPIToken(username, password, tokenName)
	if err != nil {
		return fmt.Errorf("create Technitium API token: %w", err)
	}
	if err := writeSecretFile(tokenFile, token); err != nil {
		return err
	}

	log.Printf("Technitium API token %q created and stored in %s.", tokenName, tokenFile)
	return nil
}

func runTechnitiumInit() error {
	url := mustEnv("TECHNITIUM_URL")
	username := envOrDefault("TECHNITIUM_ADMIN_USER", "admin")
	password := mustEnvOrFile("TECHNITIUM_ADMIN_PASSWORD")
	tokenName := envOrDefault("TECHNITIUM_TOKEN_NAME", defaultTechnitiumTokenName)
	tokenFile := mustEnv("TECHNITIUM_TOKEN_FILE")

	client := newTechnitiumClient(url, "")
	deadline := time.Now().Add(defaultTechnitiumInitTimeout)
	for {
		err := ensureTechnitiumToken(client, username, password, tokenName, tokenFile)
		if err == nil {
			return nil
		}
		if time.Now().Add(technitiumInitRetryInterval).After(deadline) {
			return err
		}

		log.Printf("Technitium initialization is not ready: %v; retrying...", err)
		time.Sleep(technitiumInitRetryInterval)
	}
}
