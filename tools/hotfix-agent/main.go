// Hotfix agent: reads error logs, extracts Go file paths, calls an LLM to
// generate a minimal unified diff, and outputs it for automation.
//
// Usage:
//
//	hotfix-agent [-api-key key] [-model gpt-4o] < error.log > fix.diff
//
// Reads error text from stdin.  Writes the unified diff to stdout.
// All logging goes to stderr.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const maxSourceBytes = 8192
const maxFiles = 3

func main() {
	apiKey := flag.String("api-key", os.Getenv("OPENAI_API_KEY"), "OpenAI API key")
	model := flag.String("model", "gpt-4o", "LLM model name")
	repoDir := flag.String("repo", ".", "path to repository root")
	flag.Parse()

	if *apiKey == "" {
		log.Fatal("OPENAI_API_KEY not set (use -api-key flag or env var)")
	}

	// Read error log from stdin
	errorBytes, err := io.ReadAll(os.Stdin)
	if err != nil {
		log.Fatalf("reading stdin: %v", err)
	}
	errorText := strings.TrimSpace(string(errorBytes))
	if errorText == "" {
		log.Fatal("no error text provided on stdin")
	}

	// Extract file paths from error
	filePaths := extractGoFiles(errorText)
	if len(filePaths) == 0 {
		log.Print("No Go source file paths found in error — sending error text alone.")
	}

	// Read relevant source files (capped)
	var sourceSnippets strings.Builder
	readCount := 0
	for _, fp := range filePaths {
		if readCount >= maxFiles {
			break
		}
		absPath := filepath.Join(*repoDir, fp)
		data, err := os.ReadFile(absPath)
		if err != nil {
			log.Printf("Skipping %s: %v", fp, err)
			continue
		}
		if len(data) > maxSourceBytes {
			data = data[:maxSourceBytes]
		}
		sourceSnippets.WriteString(fmt.Sprintf("\n=== %s ===\n%s\n", fp, string(data)))
		readCount++
	}

	// Assemble prompt
	prompt := buildPrompt(errorText, sourceSnippets.String())

	// Call LLM
	diff, err := callLLM(*apiKey, *model, prompt)
	if err != nil {
		log.Fatalf("LLM call failed: %v", err)
	}

	fmt.Print(diff)
}

// extractGoFiles finds Go source file paths mentioned in error text.
// Matches patterns like "db/sqlite.go:42" or "server/types.go:105".
func extractGoFiles(text string) []string {
	re := regexp.MustCompile(`([\w\-/]+\.go)(?::\d+)?`)
	seen := make(map[string]bool)
	var paths []string
	for _, match := range re.FindAllStringSubmatch(text, -1) {
		p := match[1]
		if !seen[p] && strings.HasSuffix(p, ".go") {
			seen[p] = true
			paths = append(paths, p)
		}
	}
	return paths
}

// buildPrompt assembles the system + user prompt for the LLM.
func buildPrompt(errorText, sourceSnippets string) string {
	var buf strings.Builder
	buf.WriteString("You are a Go backend engineer. A deployment of a Go + SQLite server failed with the error below.\n")
	buf.WriteString("Identify the root cause and generate the MINIMAL unified diff to fix it.\n")
	buf.WriteString("Focus on the file paths mentioned in the error.\n")
	buf.WriteString("Output ONLY a valid unified diff (git diff format) — no prose, no markdown fences, no explanation.\n")
	buf.WriteString("If a fix is obvious (missing import, typo, wrong field name), provide it.\n")
	buf.WriteString("If the error is unclear or requires human judgment, output the diff that adds a descriptive comment at the error site.\n\n")

	buf.WriteString("=== ERROR LOG ===\n")
	buf.WriteString(errorText)
	buf.WriteString("\n")

	if sourceSnippets != "" {
		buf.WriteString("\n=== RELEVANT SOURCE FILES ===\n")
		buf.WriteString(sourceSnippets)
	}

	return buf.String()
}

type chatCompletionRequest struct {
	Model     string        `json:"model"`
	Messages  []chatMessage `json:"messages"`
	MaxTokens int           `json:"max_tokens,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// callLLM sends the prompt to the OpenAI-compatible chat completions endpoint
// and returns the generated unified diff.
func callLLM(apiKey, model, prompt string) (string, error) {
	reqBody := chatCompletionRequest{
		Model: model,
		Messages: []chatMessage{
			{Role: "user", Content: prompt},
		},
		MaxTokens: 4096,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	var chatResp chatCompletionResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return "", fmt.Errorf("unmarshal response: %w", err)
	}

	if chatResp.Error != nil {
		return "", fmt.Errorf("API error: %s", chatResp.Error.Message)
	}

	if len(chatResp.Choices) == 0 {
		return "", errors.New("no choices in LLM response")
	}

	content := chatResp.Choices[0].Message.Content
	return content, nil
}

// applyDiff applies a unified diff string using `git apply`.
func applyDiff(repoDir, diff string) error {
	cmd := exec.Command("git", "apply")
	cmd.Dir = repoDir
	cmd.Stdin = strings.NewReader(diff)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git apply: %w\nstderr: %s", err, stderr.String())
	}
	return nil
}
