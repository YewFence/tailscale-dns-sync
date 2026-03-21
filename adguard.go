package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type rewriteEntry struct {
	Domain string `json:"domain"`
	Answer string `json:"answer"`
}

type adGuardClient struct {
	baseURL    string
	authHeader string
}

func newAdGuardClient(rawURL, username, password string) *adGuardClient {
	auth := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	return &adGuardClient{
		baseURL:    strings.TrimRight(rawURL, "/"),
		authHeader: "Basic " + auth,
	}
}

func (c *adGuardClient) do(method, path string, body any) ([]byte, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", c.authHeader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("adguard API error: %d %s", resp.StatusCode, respBody)
	}
	return respBody, nil
}

func (c *adGuardClient) fetchRewrites() ([]rewriteEntry, error) {
	data, err := c.do(http.MethodGet, "/control/rewrite/list", nil)
	if err != nil {
		return nil, err
	}
	var entries []rewriteEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func (c *adGuardClient) addRewrite(domain, answer string) error {
	_, err := c.do(http.MethodPost, "/control/rewrite/add", rewriteEntry{Domain: domain, Answer: answer})
	return err
}

func (c *adGuardClient) updateRewrite(domain, oldIP, newIP string) error {
	_, err := c.do(http.MethodPut, "/control/rewrite/update", map[string]any{
		"target": rewriteEntry{Domain: domain, Answer: oldIP},
		"update": rewriteEntry{Domain: domain, Answer: newIP},
	})
	return err
}

func (c *adGuardClient) deleteRewrite(domain, answer string) error {
	_, err := c.do(http.MethodPost, "/control/rewrite/delete", rewriteEntry{Domain: domain, Answer: answer})
	return err
}
