package server

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const (
	scrapeTimeout   = 5 * time.Second
	scrapeMaxBytes  = 8 * 1024 // 8 KB
	scrapeUserAgent = "binGO-buzzword-agent/1.0"
)

// privateIPNets is the list of CIDR ranges that are considered private/reserved.
// Connections to these addresses are rejected to prevent SSRF attacks.
var privateIPNets []*net.IPNet

func init() {
	cidrs := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"169.254.0.0/16",  // link-local
		"224.0.0.0/4",     // multicast
		"240.0.0.0/4",     // reserved
		"0.0.0.0/8",       // "this" network
		"100.64.0.0/10",   // CGNAT (RFC 6598)
		"192.0.0.0/24",    // IETF protocol assignments
		"198.18.0.0/15",   // benchmarking
		"198.51.100.0/24", // TEST-NET-2
		"203.0.113.0/24",  // TEST-NET-3
		"::1/128",         // IPv6 loopback
		"fe80::/10",       // IPv6 link-local
		"fc00::/7",        // IPv6 unique-local
		"ff00::/8",        // IPv6 multicast
	}
	for _, cidr := range cidrs {
		_, network, err := net.ParseCIDR(cidr)
		if err == nil {
			privateIPNets = append(privateIPNets, network)
		}
	}
}

// isPrivateIP returns true if ip falls within any private/reserved range.
func isPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return true
	}
	for _, network := range privateIPNets {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// ssrfSafeCheck resolves hostname and rejects private/reserved addresses.
func ssrfSafeCheck(hostname string) error {
	addrs, err := net.LookupHost(hostname)
	if err != nil {
		return fmt.Errorf("cannot resolve hostname %q: %w", hostname, err)
	}
	for _, addr := range addrs {
		ip := net.ParseIP(addr)
		if ip == nil {
			continue
		}
		if isPrivateIP(ip) {
			return fmt.Errorf("URL resolves to a private/reserved address — not allowed")
		}
	}
	return nil
}

// ScrapeURL fetches the visible text content from a URL, stripping HTML tags.
// It enforces SSRF protection by rejecting URLs that resolve to private or
// reserved IP addresses (RFC 1918, loopback, link-local, multicast, etc.).
// The returned text is truncated to 8 KB.
func ScrapeURL(rawURL string) (string, error) {
	return scrapeWithClient(rawURL, nil)
}

// scrapeWithClient is the internal implementation of ScrapeURL.
// When httpClient is nil a fresh SSRF-safe client is built; tests may inject
// a custom client (e.g. pointing at httptest.NewServer) to bypass DNS lookups.
func scrapeWithClient(rawURL string, httpClient *http.Client) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}

	// Only http/https schemes are allowed
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("unsupported URL scheme %q: only http and https are allowed", parsed.Scheme)
	}

	hostname := parsed.Hostname()
	if hostname == "" {
		return "", fmt.Errorf("URL has no hostname")
	}

	if httpClient == nil {
		// SSRF check on the original hostname before connecting
		if err := ssrfSafeCheck(hostname); err != nil {
			return "", err
		}
		httpClient = &http.Client{
			Timeout: scrapeTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if err := ssrfSafeCheck(req.URL.Hostname()); err != nil {
					return fmt.Errorf("redirect blocked: %w", err)
				}
				if len(via) >= 5 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		}
	}

	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("User-Agent", scrapeUserAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	limited := io.LimitReader(resp.Body, scrapeMaxBytes)

	doc, err := html.Parse(limited)
	if err != nil {
		return "", fmt.Errorf("failed to parse HTML: %w", err)
	}

	var sb strings.Builder
	extractVisibleText(doc, &sb)

	return strings.TrimSpace(sb.String()), nil
}

// extractVisibleText recursively walks an HTML node tree and collects visible text,
// skipping script, style, head, and noscript elements.
func extractVisibleText(n *html.Node, sb *strings.Builder) {
	if n.Type == html.ElementNode {
		tag := strings.ToLower(n.Data)
		if tag == "script" || tag == "style" || tag == "head" || tag == "noscript" {
			return
		}
	}
	if n.Type == html.TextNode {
		text := strings.TrimSpace(n.Data)
		if text != "" {
			sb.WriteString(text)
			sb.WriteByte(' ')
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		extractVisibleText(c, sb)
	}
}
