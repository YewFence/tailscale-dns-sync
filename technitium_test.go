package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
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
