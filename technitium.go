package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const managedRecordComment = "Managed by tailscale-dns-sync"

type technitiumZone struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Disabled bool   `json:"disabled"`
}

type technitiumRecordData struct {
	IPAddress        string `json:"ipAddress"`
	Protocol         string `json:"protocol"`
	Forwarder        string `json:"forwarder"`
	DNSSECValidation bool   `json:"dnssecValidation"`
}

type technitiumRecord struct {
	Name     string               `json:"name"`
	Type     string               `json:"type"`
	TTL      int                  `json:"ttl"`
	Disabled bool                 `json:"disabled"`
	Comments string               `json:"comments"`
	RData    technitiumRecordData `json:"rData"`
}

type technitiumAPIEnvelope struct {
	Response     json.RawMessage `json:"response"`
	Status       string          `json:"status"`
	ErrorMessage string          `json:"errorMessage"`
}

type technitiumClient struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

func newTechnitiumClient(rawURL, token string) *technitiumClient {
	return &technitiumClient{
		baseURL: strings.TrimRight(rawURL, "/"),
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *technitiumClient) do(method, path string, values url.Values, response any) error {
	requestURL := c.baseURL + path
	var body io.Reader

	if method == http.MethodGet {
		if len(values) > 0 {
			requestURL += "?" + values.Encode()
		}
	} else {
		body = strings.NewReader(values.Encode())
	}

	req, err := http.NewRequest(method, requestURL, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if method != http.MethodGet {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Technitium API error: %d %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var envelope technitiumAPIEnvelope
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return fmt.Errorf("decode Technitium API response: %w", err)
	}
	if !strings.EqualFold(envelope.Status, "ok") {
		message := envelope.ErrorMessage
		if message == "" {
			message = strings.TrimSpace(string(respBody))
		}
		return fmt.Errorf("Technitium API error: %s", message)
	}
	if response != nil && len(envelope.Response) > 0 && string(envelope.Response) != "null" {
		if err := json.Unmarshal(envelope.Response, response); err != nil {
			return fmt.Errorf("decode Technitium API payload: %w", err)
		}
	}

	return nil
}

func (c *technitiumClient) listZones() ([]technitiumZone, error) {
	var response struct {
		Zones []technitiumZone `json:"zones"`
	}
	if err := c.do(http.MethodGet, "/api/zones/list", nil, &response); err != nil {
		return nil, err
	}

	return response.Zones, nil
}

func (c *technitiumClient) createForwarderZone(zone string) error {
	values := url.Values{
		"zone":                {zone},
		"type":                {"Forwarder"},
		"initializeForwarder": {"true"},
		"protocol":            {"Udp"},
		"forwarder":           {"this-server"},
		"dnssecValidation":    {"true"},
	}

	return c.do(http.MethodPost, "/api/zones/create", values, nil)
}

func (c *technitiumClient) fetchRecords(zone string) ([]technitiumRecord, error) {
	values := url.Values{
		"domain":   {zone},
		"zone":     {zone},
		"listZone": {"true"},
	}
	var response struct {
		Zone    technitiumZone     `json:"zone"`
		Records []technitiumRecord `json:"records"`
	}
	if err := c.do(http.MethodGet, "/api/zones/records/get", values, &response); err != nil {
		return nil, err
	}

	return response.Records, nil
}

func (c *technitiumClient) ensureForwarderZone(zone string) ([]technitiumRecord, error) {
	zones, err := c.listZones()
	if err != nil {
		return nil, err
	}

	found := false
	for _, existing := range zones {
		if !strings.EqualFold(normalizeDomain(existing.Name), zone) {
			continue
		}
		found = true
		if !strings.EqualFold(existing.Type, "Forwarder") {
			return nil, fmt.Errorf("zone %s exists with type %s, expected Forwarder", zone, existing.Type)
		}
		if existing.Disabled {
			return nil, fmt.Errorf("zone %s is disabled", zone)
		}
		break
	}

	if !found {
		if err := c.createForwarderZone(zone); err != nil {
			return nil, fmt.Errorf("create Forwarder zone %s: %w", zone, err)
		}
	}

	records, err := c.fetchRecords(zone)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		if strings.EqualFold(record.Type, "FWD") &&
			!record.Disabled &&
			strings.EqualFold(record.RData.Forwarder, "this-server") {
			return records, nil
		}
	}

	return nil, fmt.Errorf("zone %s has no active FWD record using this-server", zone)
}

func (c *technitiumClient) addARecord(zone, domain, ip string, ttl int) error {
	values := url.Values{
		"zone":            {zone},
		"domain":          {domain},
		"type":            {"A"},
		"ttl":             {fmt.Sprintf("%d", ttl)},
		"overwrite":       {"false"},
		"comments":        {managedRecordComment},
		"ipAddress":       {ip},
		"ptr":             {"false"},
		"updateSvcbHints": {"false"},
	}

	return c.do(http.MethodPost, "/api/zones/records/add", values, nil)
}

func (c *technitiumClient) updateARecord(zone, domain, oldIP, newIP string, ttl int) error {
	values := url.Values{
		"zone":            {zone},
		"domain":          {domain},
		"type":            {"A"},
		"ttl":             {fmt.Sprintf("%d", ttl)},
		"disable":         {"false"},
		"comments":        {managedRecordComment},
		"ipAddress":       {oldIP},
		"newIpAddress":    {newIP},
		"ptr":             {"false"},
		"updateSvcbHints": {"false"},
	}

	return c.do(http.MethodPost, "/api/zones/records/update", values, nil)
}

func (c *technitiumClient) deleteARecord(zone, domain, ip string) error {
	values := url.Values{
		"zone":            {zone},
		"domain":          {domain},
		"type":            {"A"},
		"ipAddress":       {ip},
		"updateSvcbHints": {"false"},
	}

	return c.do(http.MethodPost, "/api/zones/records/delete", values, nil)
}
