package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestFlyConfigStaging verifies staging environment resolves to the correct
// Fly.io app name and config file.
func TestFlyConfigStaging(t *testing.T) {
	appName, configFile, err := flyConfig("staging")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if appName != flyAppStaging {
		t.Errorf("app name = %q, want %q", appName, flyAppStaging)
	}
	if configFile != "fly.staging.toml" {
		t.Errorf("config file = %q, want %q", configFile, "fly.staging.toml")
	}
}

// TestFlyConfigProduction verifies production environment resolves correctly.
func TestFlyConfigProduction(t *testing.T) {
	appName, configFile, err := flyConfig("production")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if appName != flyAppProduction {
		t.Errorf("app name = %q, want %q", appName, flyAppProduction)
	}
	if configFile != "fly.toml" {
		t.Errorf("config file = %q, want %q", configFile, "fly.toml")
	}
}

// TestFlyConfigInvalid verifies that invalid environment names are rejected
// with a descriptive error.
func TestFlyConfigInvalid(t *testing.T) {
	invalidEnvs := []string{"dev", "prod", "stage", "", "STAGING", "Production"}
	for _, env := range invalidEnvs {
		t.Run(env, func(t *testing.T) {
			_, _, err := flyConfig(env)
			if err == nil {
				t.Errorf("expected error for env %q, got nil", env)
			}
		})
	}
}

// TestRegistryBase verifies the registry constant matches expected format.
func TestRegistryBase(t *testing.T) {
	expected := "ghcr.io/jkmlnop/bingo"
	if registryBase != expected {
		t.Errorf("registryBase = %q, want %q", registryBase, expected)
	}
}

// TestDefaultGoVersion verifies the Go version constant is set.
func TestDefaultGoVersion(t *testing.T) {
	if defaultGoVersion == "" {
		t.Error("defaultGoVersion is empty")
	}
	if defaultGoVersion != "1.25.3" {
		t.Errorf("defaultGoVersion = %q, want %q", defaultGoVersion, "1.25.3")
	}
}

// TestPrintUsageDoesNotPanic verifies the help text function doesn't crash.
func TestPrintUsageDoesNotPanic(t *testing.T) {
	// printUsage writes to stderr; just verify no panic
	printUsage()
}

// TestAppURLForEnv verifies the URL mapping for each environment.
func TestAppURLForEnv(t *testing.T) {
	tests := []struct {
		env  string
		want string
	}{
		{"production", "https://bingo-server.fly.dev"},
		{"staging", "https://bingo-server-staging.fly.dev"},
		{"unknown", ""},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.env, func(t *testing.T) {
			got := appURLForEnv(tt.env)
			if got != tt.want {
				t.Errorf("appURLForEnv(%q) = %q, want %q", tt.env, got, tt.want)
			}
		})
	}
}

// TestWaitForHealthyImmediate verifies that waitForHealthy returns immediately
// when the server responds with 200 on the first attempt.
func TestWaitForHealthyImmediate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/status" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	err := waitForHealthy(t.Context(), srv.URL, 5*time.Second)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

// TestWaitForHealthyDelayed verifies that waitForHealthy polls until the
// server eventually returns 200.
func TestWaitForHealthyDelayed(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/status" {
			attempts++
			if attempts >= 3 {
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	err := waitForHealthy(t.Context(), srv.URL, 15*time.Second)
	if err != nil {
		t.Errorf("expected no error after 3 attempts, got: %v", err)
	}
	if attempts < 3 {
		t.Errorf("expected at least 3 attempts, got %d", attempts)
	}
}

// TestWaitForHealthyTimeout verifies that waitForHealthy returns an error
// when the server never becomes healthy.
func TestWaitForHealthyTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	err := waitForHealthy(t.Context(), srv.URL, 2*time.Second)
	if err == nil {
		t.Error("expected timeout error, got nil")
	}
}
