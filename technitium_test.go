package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeAPIResponse(t *testing.T, w http.ResponseWriter, response string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"response":%s,"status":"ok"}`, response)
}

func requireBearerToken(t *testing.T, r *http.Request) {
	t.Helper()
	if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
		t.Fatalf("Authorization = %q, want Bearer test-token", got)
	}
}

func requireForm(t *testing.T, r *http.Request, expected url.Values) {
	t.Helper()
	if err := r.ParseForm(); err != nil {
		t.Fatalf("ParseForm: %v", err)
	}
	for key, want := range expected {
		if got := r.Form[key]; !reflect.DeepEqual(got, want) {
			t.Fatalf("form[%q] = %v, want %v", key, got, want)
		}
	}
}

func TestEnsureForwarderZoneCreatesMissingZone(t *testing.T) {
	created := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireBearerToken(t, r)
		switch r.URL.Path {
		case "/api/zones/list":
			if r.Method != http.MethodGet {
				t.Fatalf("list method = %s, want GET", r.Method)
			}
			writeAPIResponse(t, w, `{"zones":[]}`)
		case "/api/zones/create":
			if r.Method != http.MethodPost {
				t.Fatalf("create method = %s, want POST", r.Method)
			}
			requireForm(t, r, url.Values{
				"zone":                {"ts.example.com"},
				"type":                {"Forwarder"},
				"initializeForwarder": {"true"},
				"protocol":            {"Udp"},
				"forwarder":           {"this-server"},
				"dnssecValidation":    {"true"},
			})
			created = true
			writeAPIResponse(t, w, `{"domain":"ts.example.com"}`)
		case "/api/zones/records/get":
			if !created {
				t.Fatal("records requested before zone creation")
			}
			if got := r.URL.Query().Get("listZone"); got != "true" {
				t.Fatalf("listZone = %q, want true", got)
			}
			writeAPIResponse(t, w, `{
				"zone":{"name":"ts.example.com","type":"Forwarder"},
				"records":[{"name":"ts.example.com","type":"FWD","rData":{"forwarder":"this-server","protocol":"Udp","dnssecValidation":true}}]
			}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTechnitiumClient(server.URL, "test-token")
	records, err := client.ensureForwarderZone("ts.example.com")
	if err != nil {
		t.Fatalf("ensureForwarderZone: %v", err)
	}
	if !created {
		t.Fatal("zone was not created")
	}
	if len(records) != 1 || records[0].RData.Forwarder != "this-server" {
		t.Fatalf("records = %#v", records)
	}
}

func TestEnsureForwarderZoneRejectsWrongType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireBearerToken(t, r)
		writeAPIResponse(t, w, `{"zones":[{"name":"ts.example.com","type":"Primary"}]}`)
	}))
	defer server.Close()

	client := newTechnitiumClient(server.URL, "test-token")
	_, err := client.ensureForwarderZone("ts.example.com")
	if err == nil || !strings.Contains(err.Error(), "expected Forwarder") {
		t.Fatalf("error = %v, want wrong zone type error", err)
	}
}

func TestCreateAPIToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/user/createToken" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("Authorization = %q, want empty", got)
		}
		requireForm(t, r, url.Values{
			"user":      {"admin"},
			"pass":      {"secret password"},
			"tokenName": {"tailscale-dns-sync"},
		})
		fmt.Fprint(w, `{"username":"admin","tokenName":"tailscale-dns-sync","token":"generated-token","status":"ok"}`)
	}))
	defer server.Close()

	client := newTechnitiumClient(server.URL, "")
	token, err := client.createAPIToken("admin", "secret password", "tailscale-dns-sync")
	if err != nil {
		t.Fatalf("createAPIToken: %v", err)
	}
	if token != "generated-token" {
		t.Fatalf("token = %q, want generated-token", token)
	}
}

func TestEnsureTechnitiumTokenCreatesThenReusesToken(t *testing.T) {
	createRequests := 0
	listRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/user/createToken":
			createRequests++
			fmt.Fprint(w, `{"username":"admin","tokenName":"tailscale-dns-sync","token":"generated-token","status":"ok"}`)
		case "/api/zones/list":
			listRequests++
			if got := r.Header.Get("Authorization"); got != "Bearer generated-token" {
				t.Fatalf("Authorization = %q, want Bearer generated-token", got)
			}
			writeAPIResponse(t, w, `{"zones":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	tokenFile := filepath.Join(t.TempDir(), "secrets", "token")
	client := newTechnitiumClient(server.URL, "")
	if err := ensureTechnitiumToken(client, "admin", "password", "tailscale-dns-sync", tokenFile); err != nil {
		t.Fatalf("first ensureTechnitiumToken: %v", err)
	}
	contents, err := os.ReadFile(tokenFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got := strings.TrimSpace(string(contents)); got != "generated-token" {
		t.Fatalf("token file = %q, want generated-token", got)
	}
	info, err := os.Stat(tokenFile)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("token file mode = %o, want 600", got)
	}

	if err := ensureTechnitiumToken(client, "admin", "password", "tailscale-dns-sync", tokenFile); err != nil {
		t.Fatalf("second ensureTechnitiumToken: %v", err)
	}
	if createRequests != 1 {
		t.Fatalf("create requests = %d, want 1", createRequests)
	}
	if listRequests != 1 {
		t.Fatalf("list requests = %d, want 1", listRequests)
	}
}

func TestReadEnvOrFile(t *testing.T) {
	secretFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(secretFile, []byte(" file-token \n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Setenv("TEST_TOKEN", "")
	t.Setenv("TEST_TOKEN_FILE", secretFile)
	value, err := readEnvOrFile("TEST_TOKEN")
	if err != nil {
		t.Fatalf("readEnvOrFile: %v", err)
	}
	if value != "file-token" {
		t.Fatalf("value = %q, want file-token", value)
	}

	t.Setenv("TEST_TOKEN", "environment-token")
	if _, err := readEnvOrFile("TEST_TOKEN"); err == nil {
		t.Fatal("readEnvOrFile accepted both TEST_TOKEN and TEST_TOKEN_FILE")
	}
}

func TestARecordRequests(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireBearerToken(t, r)
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		requests++
		switch r.URL.Path {
		case "/api/zones/records/add":
			requireForm(t, r, url.Values{
				"zone":            {"ts.example.com"},
				"domain":          {"device.ts.example.com"},
				"type":            {"A"},
				"ttl":             {"60"},
				"overwrite":       {"false"},
				"comments":        {managedRecordComment},
				"ipAddress":       {"100.64.0.1"},
				"ptr":             {"false"},
				"updateSvcbHints": {"false"},
			})
		case "/api/zones/records/update":
			requireForm(t, r, url.Values{
				"zone":            {"ts.example.com"},
				"domain":          {"device.ts.example.com"},
				"type":            {"A"},
				"ttl":             {"60"},
				"disable":         {"false"},
				"comments":        {managedRecordComment},
				"ipAddress":       {"100.64.0.1"},
				"newIpAddress":    {"100.64.0.2"},
				"ptr":             {"false"},
				"updateSvcbHints": {"false"},
			})
		case "/api/zones/records/delete":
			requireForm(t, r, url.Values{
				"zone":            {"ts.example.com"},
				"domain":          {"device.ts.example.com"},
				"type":            {"A"},
				"ipAddress":       {"100.64.0.2"},
				"updateSvcbHints": {"false"},
			})
		default:
			http.NotFound(w, r)
			return
		}
		writeAPIResponse(t, w, `{}`)
	}))
	defer server.Close()

	client := newTechnitiumClient(server.URL, "test-token")
	if err := client.addARecord("ts.example.com", "device.ts.example.com", "100.64.0.1", 60); err != nil {
		t.Fatalf("addARecord: %v", err)
	}
	if err := client.updateARecord("ts.example.com", "device.ts.example.com", "100.64.0.1", "100.64.0.2", 60); err != nil {
		t.Fatalf("updateARecord: %v", err)
	}
	if err := client.deleteARecord("ts.example.com", "device.ts.example.com", "100.64.0.2"); err != nil {
		t.Fatalf("deleteARecord: %v", err)
	}
	if requests != 3 {
		t.Fatalf("requests = %d, want 3", requests)
	}
}

func TestTechnitiumAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"status":"error","errorMessage":"permission denied"}`)
	}))
	defer server.Close()

	client := newTechnitiumClient(server.URL, "test-token")
	_, err := client.listZones()
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("error = %v, want API error message", err)
	}
}

func TestDesiredARecords(t *testing.T) {
	got := desiredARecords(map[string]string{
		"Device-A": "100.64.0.1",
	}, "TS.Example.COM")
	want := map[string]string{
		"device-a.ts.example.com":   "100.64.0.1",
		"*.device-a.ts.example.com": "100.64.0.1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("desiredARecords = %#v, want %#v", got, want)
	}
}
