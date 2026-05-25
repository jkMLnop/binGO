package server

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestIsPrivateIP verifies that known private/reserved addresses are rejected.
func TestIsPrivateIP(t *testing.T) {
	private := []string{
		"127.0.0.1",
		"10.0.0.1",
		"172.16.0.1",
		"172.31.255.255",
		"192.168.1.1",
		"169.254.0.1", // link-local
		"224.0.0.1",   // multicast
		"::1",         // IPv6 loopback
		"fe80::1",     // IPv6 link-local
		"fc00::1",     // IPv6 unique-local
		"ff02::1",     // IPv6 multicast
		"100.64.0.1",  // CGNAT
		"198.18.0.1",  // benchmarking
		"240.0.0.1",   // reserved
	}
	for _, addr := range private {
		ip := net.ParseIP(addr)
		if ip == nil {
			t.Fatalf("failed to parse test IP %q", addr)
		}
		if !isPrivateIP(ip) {
			t.Errorf("expected %q to be private, but isPrivateIP returned false", addr)
		}
	}
}

// TestIsPrivateIPPublic verifies that real public IPs are not flagged.
func TestIsPrivateIPPublic(t *testing.T) {
	public := []string{
		"8.8.8.8",
		"1.1.1.1",
		"208.67.222.222",
		"2001:4860:4860::8888", // Google IPv6
	}
	for _, addr := range public {
		ip := net.ParseIP(addr)
		if ip == nil {
			t.Fatalf("failed to parse test IP %q", addr)
		}
		if isPrivateIP(ip) {
			t.Errorf("expected %q to be public, but isPrivateIP returned true", addr)
		}
	}
}

// TestScrapeURLSchemeRejection verifies that non-http(s) schemes are rejected.
func TestScrapeURLSchemeRejection(t *testing.T) {
	bad := []string{
		"file:///etc/passwd",
		"ftp://example.com/file",
		"gopher://example.com/",
		"javascript:alert(1)",
	}
	for _, u := range bad {
		_, err := ScrapeURL(u)
		if err == nil {
			t.Errorf("expected error for scheme in URL %q, got nil", u)
		}
	}
}

// TestScrapeURLSSRFRejection verifies that loopback/private URLs are rejected.
func TestScrapeURLSSRFRejection(t *testing.T) {
	// These hostnames resolve locally — the SSRF check must fire before any
	// connection is attempted. We test the URL parsing + rejection path.
	bad := []string{
		"http://127.0.0.1/admin",
		"http://localhost/secret",
		"http://[::1]/internal",
	}
	for _, u := range bad {
		_, err := ScrapeURL(u)
		if err == nil {
			t.Errorf("expected SSRF rejection for %q, got nil", u)
		}
	}
}

// TestScrapeURLHTMLStripping verifies that a mock HTTP server's HTML is scraped
// and script/style content is removed.
func TestScrapeURLHTMLStripping(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html>
<head><title>Ignored</title><style>body{color:red}</style></head>
<body>
  <script>alert('ignored')</script>
  <h1>Hello World</h1>
  <p>This is visible text.</p>
  <noscript>Also ignored</noscript>
</body>
</html>`))
	}))
	defer srv.Close()

	// Inject the httptest server's own client so the SSRF DNS check is skipped.
	text, err := scrapeWithClient(srv.URL, srv.Client())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(text, "Hello World") {
		t.Errorf("expected 'Hello World' in scraped text, got: %q", text)
	}
	if !strings.Contains(text, "This is visible text") {
		t.Errorf("expected body text in scraped text, got: %q", text)
	}
	if strings.Contains(text, "alert") {
		t.Errorf("script content should be stripped, got: %q", text)
	}
	if strings.Contains(text, "color:red") {
		t.Errorf("style content should be stripped, got: %q", text)
	}
	if strings.Contains(text, "Also ignored") {
		t.Errorf("noscript content should be stripped, got: %q", text)
	}
}

// TestScrapeURLTruncation verifies the 8 KB limit.
func TestScrapeURLTruncation(t *testing.T) {
	large := strings.Repeat("word ", 2000) // ~10 KB
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(large))
	}))
	defer srv.Close()

	text, err := scrapeWithClient(srv.URL, srv.Client())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(text) > scrapeMaxBytes+100 {
		t.Errorf("expected output ≤ %d bytes, got %d", scrapeMaxBytes, len(text))
	}
}
