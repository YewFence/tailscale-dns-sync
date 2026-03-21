package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type tailscaleDevice struct {
	Hostname  string   `json:"hostname"`
	Addresses []string `json:"addresses"`
}

type tailscaleDevicesResponse struct {
	Devices []tailscaleDevice `json:"devices"`
}

func fetchTailscaleDevices(apiKey, tailnet string) (map[string]string, error) {
	u := fmt.Sprintf("https://api.tailscale.com/api/v2/tailnet/%s/devices", url.PathEscape(tailnet))
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("tailscale API error: %d %s", resp.StatusCode, body)
	}

	var data tailscaleDevicesResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	devices := make(map[string]string)
	for _, d := range data.Devices {
		for _, addr := range d.Addresses {
			if strings.HasPrefix(addr, "100.") {
				devices[strings.ToLower(d.Hostname)] = addr
				break
			}
		}
	}
	return devices, nil
}
