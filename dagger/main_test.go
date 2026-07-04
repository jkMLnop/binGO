package main

import (
	"testing"
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
