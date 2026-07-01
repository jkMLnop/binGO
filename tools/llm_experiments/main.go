package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// deepSeekRequest is the JSON body for POST /chat/completions.
type deepSeekRequest struct {
	Model          string        `json:"model"`
	Messages       []chatMessage `json:"messages"`
	Stream         bool          `json:"stream"`
	ResponseFormat *struct {
		Type string `json:"type"`
	} `json:"response_format,omitempty"`
	Thinking *struct {
		Type string `json:"type"`
	} `json:"thinking,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`
	TopP        *float64 `json:"top_p,omitempty"`
}

type experimentCase struct {
	SetCount    int
	WordsPerSet int
}

type experimentResult struct {
	Timestamp     string          `json:"timestamp"`
	Model         string          `json:"model"`
	BaseURL       string          `json:"base_url"`
	Topic         string          `json:"topic"`
	URL           string          `json:"url,omitempty"`
	ScrapeMode    string          `json:"scrape_mode"`
	ContextChars  int             `json:"context_chars"`
	Attempt       int             `json:"attempt"`
	SetCount      int             `json:"set_count"`
	WordsPerSet   int             `json:"words_per_set"`
	DurationMs    int64           `json:"duration_ms"`
	RawResponse   string          `json:"raw_response"`
	OutputText    string          `json:"output_text"`
	JSONValid     bool            `json:"json_valid"`
	ShapeStatus   string          `json:"shape_status,omitempty"`
	ObservedSets  int             `json:"observed_sets,omitempty"`
	ObservedWords int             `json:"observed_words,omitempty"`
	WordDelta     int             `json:"word_delta,omitempty"`
	ParsedError   string          `json:"parsed_error,omitempty"`
	ParsedSummary json.RawMessage `json:"parsed_summary,omitempty"`
}

type experimentSummary struct {
	Case          string
	ScrapeMode    string
	ContextChars  int
	SetCount      int
	WordsPerSet   int
	Attempt       int
	DurationMs    int64
	JSONValid     bool
	ShapeStatus   string
	ObservedSets  int
	ObservedWords int
	WordDelta     int
	ParsedError   string
	JSONPath      string
	TextPath      string
}

type scrapeOptions struct {
	Delay      time.Duration
	MaxRetries int
}

func main() {
	var (
		baseURL           = flag.String("base-url", "https://api.deepseek.com", "DeepSeek API base URL")
		apiKey            = flag.String("api-key", "", "DeepSeek API key (required)")
		model             = flag.String("model", "deepseek-v4-pro", "DeepSeek model name")
		topic             = flag.String("topic", "anime convention", "Topic to generate buzzwords for")
		urlValue          = flag.String("url", "", "Optional URL to scrape for additional context")
		outputDir         = flag.String("output-dir", "docs/llm-experiments", "Directory for experiment outputs")
		cases             = flag.String("cases", "1x100", "Comma-separated set/word pairs to test, formatted as NxM")
		thinkingEnabled   = flag.Bool("think", false, "Enable thinking mode")
		temperature       = flag.Float64("temperature", 0, "Sampling temperature (0=omit/use API default)")
		topP              = flag.Float64("top-p", 0, "Top-p sampling (0=omit/use API default)")
		maxTokens         = flag.Int("max-tokens", 4096, "Max output tokens for each generation")
		scrapeMode        = flag.String("scrape-mode", "single", "Scrape mode: none,single,recursive,visual")
		visualContextFile = flag.String("visual-context-file", "", "Path to a text file containing visual scrape context")
		recursiveMaxPages = flag.Int("recursive-max-pages", 5, "Maximum number of pages to crawl for recursive mode")
		recursiveMaxDepth = flag.Int("recursive-max-depth", 1, "Maximum link depth for recursive mode")
		maxContextChars   = flag.Int("max-context-chars", 12000, "Maximum context characters kept from scraped content")
		crawlDelayMs      = flag.Int("crawl-delay-ms", 300, "Delay between scrape requests in milliseconds")
		scrapeRetries     = flag.Int("scrape-retries", 2, "Retry count for throttled scrape responses (429/503)")
		caseMaxAttempts   = flag.Int("case-max-attempts", 4, "Maximum auto-repair attempts per experiment case")
		llmTimeoutSec     = flag.Int("llm-timeout-sec", 600, "Timeout in seconds for each DeepSeek generation request")
	)
	flag.Parse()

	if strings.TrimSpace(*apiKey) == "" {
		fatalf("--api-key is required (set DEEPSEEK_API_KEY or pass --api-key)")
	}

	runsDir := filepath.Join(*outputDir, time.Now().Format("2006-01-02_150405"))
	if err := os.MkdirAll(runsDir, 0o755); err != nil {
		fatalf("create output dir: %v", err)
	}

	testCases := mustParseCases(*cases)

	opts := scrapeOptions{Delay: time.Duration(*crawlDelayMs) * time.Millisecond, MaxRetries: *scrapeRetries}
	contextMap, contextErrs := prepareContexts([]string{*scrapeMode}, *urlValue, *visualContextFile, *recursiveMaxPages, *recursiveMaxDepth, *maxContextChars, opts)
	scraped := contextMap[*scrapeMode]
	summaries := make([]experimentSummary, 0, len(testCases))

	for _, experimentCase := range testCases {
		if experimentCase.SetCount <= 0 || experimentCase.WordsPerSet <= 0 {
			continue
		}
		name := fmt.Sprintf(
			"think-%v_temp-%.1f_topp-%.1f_sets-%d_words-%d",
			*thinkingEnabled, *temperature, *topP, experimentCase.SetCount, experimentCase.WordsPerSet,
		)

		result := experimentResult{
			Timestamp:    time.Now().Format(time.RFC3339Nano),
			Model:        *model,
			BaseURL:      *baseURL,
			Topic:        *topic,
			URL:          *urlValue,
			ScrapeMode:   *scrapeMode,
			ContextChars: len(scraped),
			Attempt:      0,
			SetCount:     experimentCase.SetCount,
			WordsPerSet:  experimentCase.WordsPerSet,
		}

		fmt.Printf("starting %s\n", name)
		if err := contextErrs[*scrapeMode]; err != nil {
			result.JSONValid = false
			result.ShapeStatus = "context_error"
			result.ParsedError = err.Error()
		} else {
			result = runExperimentWithRetries(*baseURL, *apiKey, *model, *topic, *urlValue, scraped, experimentCase.SetCount, experimentCase.WordsPerSet, *thinkingEnabled, *temperature, *topP, *maxTokens, *caseMaxAttempts, time.Duration(*llmTimeoutSec)*time.Second)
		}

		jsonPath := filepath.Join(runsDir, name+".json")
		textPath := filepath.Join(runsDir, name+".txt")
		writeJSON(jsonPath, result)
		writeText(textPath, result.OutputText)
		summaries = append(summaries, experimentSummary{
			Case:          fmt.Sprintf("%dx%d", experimentCase.SetCount, experimentCase.WordsPerSet),
			ScrapeMode:    *scrapeMode,
			ContextChars:  len(scraped),
			SetCount:      experimentCase.SetCount,
			WordsPerSet:   experimentCase.WordsPerSet,
			Attempt:       result.Attempt,
			DurationMs:    result.DurationMs,
			JSONValid:     result.JSONValid,
			ShapeStatus:   result.ShapeStatus,
			ObservedSets:  result.ObservedSets,
			ObservedWords: result.ObservedWords,
			WordDelta:     result.WordDelta,
			ParsedError:   result.ParsedError,
			JSONPath:      jsonPath,
			TextPath:      textPath,
		})
		fmt.Printf("%s | valid=%t | %s | %dms | %s\n", name, result.JSONValid, result.ShapeStatus, result.DurationMs, result.ParsedError)
	}

	writeSummaryCSV(filepath.Join(runsDir, "summary.csv"), summaries)
	writeContextsDebug(filepath.Join(runsDir, "contexts.json"), contextMap)
	fmt.Printf("saved results to %s\n", runsDir)
}

func runExperimentWithRetries(baseURL, apiKey, model, topic, urlValue, scraped string, setCount, wordsPerSet int, thinkingEnabled bool, temperature, topP float64, maxTokens, maxAttempts int, llmTimeout time.Duration) experimentResult {
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	result := experimentResult{}
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		suffix := strictPromptSuffix(attempt)
		result = runExperiment(baseURL, apiKey, model, topic, urlValue, scraped, setCount, wordsPerSet, thinkingEnabled, temperature, topP, maxTokens, attempt, suffix, llmTimeout)
		if isMeaningfulPass(result) {
			return result
		}
	}
	return result
}

func strictPromptSuffix(attempt int) string {
	switch attempt {
	case 1:
		return ""
	case 2:
		return "CRITICAL: Return ONLY a JSON object matching the schema. Do not include reasoning, markdown, code fences, or extra keys."
	case 3:
		return "CRITICAL: No thinking text. Output must start with '{' and end with '}'. Ensure exact set count and exact words-per-set."
	default:
		return "CRITICAL: Return strictly valid JSON only. If needed, reuse safe placeholder phrases to reach exact counts. No prose."
	}
}

func isMeaningfulPass(result experimentResult) bool {
	return result.JSONValid && result.ShapeStatus == "exact" && strings.TrimSpace(result.OutputText) != ""
}

func runExperiment(baseURL, apiKey, model, topic, urlValue, scraped string, setCount, wordsPerSet int, thinkingEnabled bool, temperature, topP float64, maxTokens, attempt int, promptSuffix string, llmTimeout time.Duration) experimentResult {
	systemPrompt := fmt.Sprintf(
		"You are a bingo card designer. Generate a themed buzzword list for a scavenger-hunt style bingo game. The words should be short observable things (2-5 words), specific to the topic, and fun to spot in person. Output %d set(s) of exactly %d words each. Return JSON only in the form {\"sets\":[{\"label\":\"Set A\",\"words\":[...]}]}",
		setCount,
		wordsPerSet,
	)
	if strings.TrimSpace(promptSuffix) != "" {
		systemPrompt += "\n\n" + strings.TrimSpace(promptSuffix)
	}
	userPrompt := buildUserPrompt(topic, urlValue, scraped)

	result := experimentResult{
		Timestamp:    time.Now().Format(time.RFC3339Nano),
		Model:        model,
		BaseURL:      baseURL,
		Topic:        topic,
		URL:          urlValue,
		ContextChars: len(scraped),
		Attempt:      attempt,
		SetCount:     setCount,
		WordsPerSet:  wordsPerSet,
	}

	if strings.TrimSpace(userPrompt) == "" {
		result.JSONValid = false
		result.ShapeStatus = "prompt_error"
		result.ParsedError = "empty user prompt after input assembly"
		return result
	}

	// Build DeepSeek-compatible request body.
	var tempPtr, topPPtr *float64
	if temperature != 0 {
		tempPtr = &temperature
	}
	if topP != 0 {
		topPPtr = &topP
	}
	body := deepSeekRequest{
		Model:    model,
		Messages: []chatMessage{{Role: "system", Content: systemPrompt}, {Role: "user", Content: userPrompt}},
		Stream:   false,
		ResponseFormat: &struct {
			Type string `json:"type"`
		}{Type: "json_object"},
		Thinking: &struct {
			Type string `json:"type"`
		}{
			Type: map[bool]string{true: "enabled", false: "disabled"}[thinkingEnabled],
		},
		Temperature: tempPtr,
		TopP:        topPPtr,
	}

	payload, err := json.Marshal(body)
	if err != nil {
		result.JSONValid = false
		result.ShapeStatus = "request_error"
		result.ParsedError = err.Error()
		return result
	}

	start := time.Now()
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(baseURL, "/")+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		result.JSONValid = false
		result.ShapeStatus = "request_error"
		result.ParsedError = err.Error()
		return result
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: llmTimeout}
	resp, err := client.Do(req)
	if err != nil {
		result.JSONValid = false
		result.ShapeStatus = "transport_error"
		result.ParsedError = err.Error()
		return result
	}
	defer resp.Body.Close()

	all, err := io.ReadAll(resp.Body)
	result.DurationMs = time.Since(start).Milliseconds()
	result.RawResponse = string(all)
	if err != nil {
		result.JSONValid = false
		result.ShapeStatus = "transport_error"
		result.ParsedError = err.Error()
		return result
	}

	var chatResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(all, &chatResp); err != nil {
		result.JSONValid = false
		result.ShapeStatus = "invalid_json"
		result.ParsedError = err.Error()
		return result
	}
	if chatResp.Error != nil && strings.TrimSpace(chatResp.Error.Message) != "" {
		result.JSONValid = false
		result.ShapeStatus = "model_error"
		result.ParsedError = chatResp.Error.Message
		return result
	}
	if len(chatResp.Choices) == 0 {
		result.JSONValid = false
		result.ShapeStatus = "no_choices"
		result.ParsedError = "no choices in DeepSeek response"
		return result
	}

	result.OutputText = chatResp.Choices[0].Message.Content
	result.JSONValid = json.Valid([]byte(result.OutputText))
	if !result.JSONValid {
		result.ShapeStatus = "invalid_json"
		result.ParsedError = "assistant output is not valid JSON"
		return result
	}
	if err := json.Unmarshal([]byte(result.OutputText), &result.ParsedSummary); err != nil {
		result.JSONValid = false
		result.ShapeStatus = "invalid_json"
		result.ParsedError = err.Error()
		return result
	}

	shapeStatus, observedSets, observedWords, wordDelta, shapeErr := classifyGeneratedSets([]byte(result.OutputText), setCount, wordsPerSet)
	result.ShapeStatus = shapeStatus
	result.ObservedSets = observedSets
	result.ObservedWords = observedWords
	result.WordDelta = wordDelta
	if shapeErr != nil {
		result.JSONValid = false
		result.ParsedError = shapeErr.Error()
		return result
	}
	return result
}

func buildUserPrompt(topic, urlValue, scraped string) string {
	topicPart := strings.TrimSpace(topic)
	scrapePart := strings.TrimSpace(scraped)
	if scrapePart != "" {
		scrapePart = fmt.Sprintf("Scraped context (single")
		if strings.TrimSpace(urlValue) != "" {
			scrapePart += fmt.Sprintf(" from %s", strings.TrimSpace(urlValue))
		}
		scrapePart += "):\n" + strings.TrimSpace(scraped)
	}
	if topicPart != "" {
		topicPart = "Topic description:\n" + topicPart
	}

	parts := make([]string, 0, 2)
	if scrapePart != "" {
		parts = append(parts, scrapePart)
	}
	if topicPart != "" {
		parts = append(parts, topicPart)
	}

	return strings.Join(parts, "\n\n")
}

func prepareContexts(modes []string, urlValue, visualContextFile string, recursiveMaxPages, recursiveMaxDepth, maxContextChars int, opts scrapeOptions) (map[string]string, map[string]error) {
	contexts := make(map[string]string, len(modes))
	errs := make(map[string]error, len(modes))

	for _, mode := range modes {
		switch mode {
		case "none":
			contexts[mode] = ""
		case "single":
			if strings.TrimSpace(urlValue) == "" {
				errs[mode] = fmt.Errorf("single mode requires --url")
				continue
			}
			text, err := scrapeSingle(urlValue, maxContextChars, opts)
			if err != nil {
				errs[mode] = err
				continue
			}
			contexts[mode] = text
		case "recursive":
			if strings.TrimSpace(urlValue) == "" {
				errs[mode] = fmt.Errorf("recursive mode requires --url")
				continue
			}
			text, err := scrapeRecursive(urlValue, recursiveMaxPages, recursiveMaxDepth, maxContextChars, opts)
			if err != nil {
				errs[mode] = err
				continue
			}
			contexts[mode] = text
		case "visual":
			if strings.TrimSpace(visualContextFile) == "" {
				errs[mode] = fmt.Errorf("visual mode requires --visual-context-file")
				continue
			}
			data, err := os.ReadFile(visualContextFile)
			if err != nil {
				errs[mode] = fmt.Errorf("read visual context file: %w", err)
				continue
			}
			contexts[mode] = trimTo(strings.TrimSpace(string(data)), maxContextChars)
		default:
			errs[mode] = fmt.Errorf("unsupported scrape mode %q", mode)
		}
	}

	return contexts, errs
}

func scrapeSingle(rawURL string, maxChars int, opts scrapeOptions) (string, error) {
	text, _, err := fetchPageTextAndLinks(rawURL, opts)
	if err != nil {
		return "", err
	}
	return trimTo(text, maxChars), nil
}

func scrapeRecursive(rawURL string, maxPages, maxDepth, maxChars int, opts scrapeOptions) (string, error) {
	if maxPages <= 0 {
		maxPages = 1
	}
	if maxDepth < 0 {
		maxDepth = 0
	}

	seed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	if seed.Scheme != "http" && seed.Scheme != "https" {
		return "", fmt.Errorf("unsupported URL scheme %q", seed.Scheme)
	}

	type item struct {
		url   string
		depth int
	}
	queue := []item{{url: seed.String(), depth: 0}}
	visited := map[string]bool{}
	var pages []string

	for len(queue) > 0 && len(pages) < maxPages {
		current := queue[0]
		queue = queue[1:]
		if visited[current.url] {
			continue
		}
		visited[current.url] = true

		text, links, err := fetchPageTextAndLinks(current.url, opts)
		if err != nil {
			continue
		}
		if opts.Delay > 0 {
			time.Sleep(opts.Delay)
		}
		if strings.TrimSpace(text) != "" {
			pages = append(pages, fmt.Sprintf("[Page %d] %s\n%s", len(pages)+1, current.url, strings.TrimSpace(text)))
		}
		if current.depth >= maxDepth {
			continue
		}
		for _, next := range links {
			nextURL, err := url.Parse(next)
			if err != nil {
				continue
			}
			if !strings.EqualFold(nextURL.Hostname(), seed.Hostname()) {
				continue
			}
			if !visited[nextURL.String()] {
				queue = append(queue, item{url: nextURL.String(), depth: current.depth + 1})
			}
		}
	}

	if len(pages) == 0 {
		return "", fmt.Errorf("no readable pages collected in recursive crawl")
	}
	return trimTo(strings.Join(pages, "\n\n"), maxChars), nil
}

func fetchPageTextAndLinks(rawURL string, opts scrapeOptions) (string, []string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", nil, fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", nil, fmt.Errorf("unsupported URL scheme %q", parsed.Scheme)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	maxAttempts := opts.MaxRetries + 1
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		req, err := http.NewRequest(http.MethodGet, rawURL, nil)
		if err != nil {
			return "", nil, fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("User-Agent", "binGO-llm-experiment/1.0")

		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("fetch URL: %w", err)
			if attempt < maxAttempts {
				backoff := time.Duration(attempt) * opts.Delay
				if backoff > 0 {
					time.Sleep(backoff)
				}
				continue
			}
			break
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
			lastErr = fmt.Errorf("throttled with HTTP %d", resp.StatusCode)
			resp.Body.Close()
			if attempt < maxAttempts {
				backoff := time.Duration(attempt) * opts.Delay
				if backoff > 0 {
					time.Sleep(backoff)
				}
				continue
			}
			break
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			resp.Body.Close()
			return "", nil, fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
		}

		limited := io.LimitReader(resp.Body, 256*1024)
		doc, parseErr := html.Parse(limited)
		resp.Body.Close()
		if parseErr != nil {
			return "", nil, fmt.Errorf("parse HTML: %w", parseErr)
		}

		var textBuilder strings.Builder
		extractVisibleText(doc, &textBuilder)
		links := extractLinks(doc, parsed)
		return strings.TrimSpace(textBuilder.String()), links, nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("failed to fetch URL")
	}
	return "", nil, lastErr
}

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

func extractLinks(doc *html.Node, base *url.URL) []string {
	seen := map[string]bool{}
	out := make([]string, 0, 32)
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && strings.EqualFold(n.Data, "a") {
			for _, attr := range n.Attr {
				if !strings.EqualFold(attr.Key, "href") {
					continue
				}
				href := strings.TrimSpace(attr.Val)
				if href == "" || strings.HasPrefix(href, "#") {
					continue
				}
				rel, err := url.Parse(href)
				if err != nil {
					continue
				}
				abs := base.ResolveReference(rel)
				if abs.Scheme != "http" && abs.Scheme != "https" {
					continue
				}
				abs.Fragment = ""
				normalized := abs.String()
				if !seen[normalized] {
					seen[normalized] = true
					out = append(out, normalized)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return out
}

func classifyGeneratedSets(output []byte, expectedSets, expectedWordsPerSet int) (string, int, int, int, error) {
	var parsed struct {
		Sets []struct {
			Label string   `json:"label"`
			Words []string `json:"words"`
		} `json:"sets"`
	}
	if err := json.Unmarshal(output, &parsed); err != nil {
		return "invalid_json", 0, 0, 0, err
	}

	observedSets := len(parsed.Sets)
	observedWords := 0
	shortFound := false
	overFound := false
	for _, set := range parsed.Sets {
		observedWords += len(set.Words)
		if len(set.Words) < expectedWordsPerSet {
			shortFound = true
		}
		if len(set.Words) > expectedWordsPerSet {
			overFound = true
		}
	}

	wordDelta := observedWords - (expectedSets * expectedWordsPerSet)
	if observedSets != expectedSets {
		return "wrong_set_count", observedSets, observedWords, wordDelta, fmt.Errorf("expected %d sets, got %d", expectedSets, observedSets)
	}
	if shortFound && overFound {
		return "mixed_shape", observedSets, observedWords, wordDelta, fmt.Errorf("output mixed short and oversized sets")
	}
	if overFound {
		return "oversized", observedSets, observedWords, wordDelta, fmt.Errorf("output oversized by %d words", wordDelta)
	}
	if shortFound {
		return "short", observedSets, observedWords, wordDelta, fmt.Errorf("output short by %d words", -wordDelta)
	}
	return "exact", observedSets, observedWords, wordDelta, nil
}

func mustParseCases(value string) []experimentCase {
	parts := strings.Split(value, ",")
	results := make([]experimentCase, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		fields := strings.Split(part, "x")
		if len(fields) != 2 {
			fatalf("parse case %q: expected NxM format", part)
		}
		setCount, err := strconv.Atoi(strings.TrimSpace(fields[0]))
		if err != nil {
			fatalf("parse case %q: %v", part, err)
		}
		wordsPerSet, err := strconv.Atoi(strings.TrimSpace(fields[1]))
		if err != nil {
			fatalf("parse case %q: %v", part, err)
		}
		results = append(results, experimentCase{SetCount: setCount, WordsPerSet: wordsPerSet})
	}
	return results
}

func trimTo(s string, maxChars int) string {
	if maxChars <= 0 || len(s) <= maxChars {
		return s
	}
	return s[:maxChars]
}

func writeJSON(path string, value any) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		fatalf("marshal %s: %v", path, err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		fatalf("write %s: %v", path, err)
	}
}

func writeText(path, value string) {
	if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
		fatalf("write %s: %v", path, err)
	}
}

func writeSummaryCSV(path string, summaries []experimentSummary) {
	file, err := os.Create(path)
	if err != nil {
		fatalf("create %s: %v", path, err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	headers := []string{
		"case", "scrape_mode", "context_chars", "set_count", "words_per_set", "attempt", "duration_ms", "json_valid", "shape_status", "observed_sets", "observed_words", "word_delta", "parsed_error", "json_path", "text_path",
	}
	if err := writer.Write(headers); err != nil {
		fatalf("write summary header: %v", err)
	}
	for _, summary := range summaries {
		if err := writer.Write([]string{
			summary.Case,
			summary.ScrapeMode,
			strconv.Itoa(summary.ContextChars),
			strconv.Itoa(summary.SetCount),
			strconv.Itoa(summary.WordsPerSet),
			strconv.Itoa(summary.Attempt),
			strconv.FormatInt(summary.DurationMs, 10),
			strconv.FormatBool(summary.JSONValid),
			summary.ShapeStatus,
			strconv.Itoa(summary.ObservedSets),
			strconv.Itoa(summary.ObservedWords),
			strconv.Itoa(summary.WordDelta),
			summary.ParsedError,
			summary.JSONPath,
			summary.TextPath,
		}); err != nil {
			fatalf("write summary row: %v", err)
		}
	}
	if err := writer.Error(); err != nil {
		fatalf("flush summary csv: %v", err)
	}
}

func writeContextsDebug(path string, contexts map[string]string) {
	type row struct {
		Mode    string `json:"mode"`
		Chars   int    `json:"chars"`
		Preview string `json:"preview"`
	}
	rows := make([]row, 0, len(contexts))
	for mode, text := range contexts {
		preview := text
		if len(preview) > 400 {
			preview = preview[:400]
		}
		rows = append(rows, row{Mode: mode, Chars: len(text), Preview: preview})
	}
	writeJSON(path, rows)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
