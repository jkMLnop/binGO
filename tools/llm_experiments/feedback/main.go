package main

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type generated struct {
	Sets []struct {
		Label string   `json:"label"`
		Words []string `json:"words"`
	} `json:"sets"`
}

type feedbackRow struct {
	Timestamp string
	File      string
	// Legacy fields (old filename format: mode-..._order-..._sets-..._words-..._tokens-...)
	Mode       string
	InputOrder string
	Tokens     int
	// Current fields (new filename format: think-..._temp-..._topp-..._sets-..._words-...)
	Think       string
	Temperature string
	TopP        string
	// Common fields
	SetCount           int
	WordsPerSet        int
	UniqueWords        int
	TotalWords         int
	DuplicatePct       float64
	SocialTermHits     int
	ObservableTermHits int
	ObservableScore    int
	FunScore           int
	SocialScore        int
	VarietyScore       int
	RelevanceScore     int
	UserWeightedScore  float64
	HybridScore        float64
	Notes              string
}

// oldFilenamePattern matches legacy harness output: mode-..._order-..._sets-N_words-M_tokens-T
var oldFilenamePattern = regexp.MustCompile(`mode-([^_]+)_order-([^_]+)_sets-(\d+)_words-(\d+)_tokens-(\d+)`)

// newFilenamePattern matches current harness output: think-{bool}_temp-{float}_topp-{float}_sets-N_words-M
var newFilenamePattern = regexp.MustCompile(`think-(true|false)_temp-([\d.]+)_topp-([\d.]+)_sets-(\d+)_words-(\d+)`)

var socialTerms = []string{
	"facebook", "instagram", "tiktok", "threads", "discord", "bluesky", "social media", "forum",
}

var observableTerms = []string{
	"booth", "panel", "badge", "lanyard", "cosplay", "stage", "line", "vendor", "artist alley", "hall", "poster", "mascot", "props", "photo", "workshop", "gameshow", "ticket", "wristband", "queue", "signage", "merch", "table", "performance",
}

func main() {
	file := flag.String("file", "", "Path to generated output JSON/TXT file")
	out := flag.String("out", "docs/llm-experiments/user-feedback.csv", "Feedback CSV path")
	observable := flag.Int("observable", 0, "User score 1-5: how observable/spot-able items are")
	fun := flag.Int("fun", 0, "User score 1-5: scavenger hunt fun")
	social := flag.Int("social", 0, "User score 1-5: social interaction potential")
	variety := flag.Int("variety", 0, "User score 1-5: variety/non-repetition")
	relevance := flag.Int("relevance", 0, "User score 1-5: relevance to event")
	notes := flag.String("notes", "", "Optional notes")
	summary := flag.Bool("summary", false, "Show aggregate feedback summary")
	flag.Parse()

	if *summary {
		if err := printSummary(*out); err != nil {
			fatal(err)
		}
		return
	}

	if strings.TrimSpace(*file) == "" {
		fatal(errors.New("--file is required unless --summary is set"))
	}

	manualScores := []int{*observable, *fun, *social, *variety, *relevance}
	for _, s := range manualScores {
		if s < 1 || s > 5 {
			fatal(errors.New("all user score flags must be between 1 and 5"))
		}
	}

	row, err := scoreFile(*file)
	if err != nil {
		fatal(err)
	}
	row.ObservableScore = *observable
	row.FunScore = *fun
	row.SocialScore = *social
	row.VarietyScore = *variety
	row.RelevanceScore = *relevance
	row.Notes = strings.TrimSpace(*notes)
	row.UserWeightedScore = userWeightedScore(row)
	row.HybridScore = hybridScore(row)

	if err := appendFeedback(*out, row); err != nil {
		fatal(err)
	}

	fmt.Printf("saved feedback to %s\n", *out)
	fmt.Printf("user_score=%.1f hybrid_score=%.1f dup_pct=%.1f social_hits=%d observable_hits=%d\n",
		row.UserWeightedScore, row.HybridScore, row.DuplicatePct, row.SocialTermHits, row.ObservableTermHits)
}

func scoreFile(path string) (feedbackRow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return feedbackRow{}, fmt.Errorf("read file: %w", err)
	}

	var g generated
	if err := json.Unmarshal(data, &g); err != nil {
		return feedbackRow{}, fmt.Errorf("parse generated JSON: %w", err)
	}

	all := make([]string, 0, 128)
	for _, set := range g.Sets {
		for _, word := range set.Words {
			all = append(all, normalize(word))
		}
	}
	if len(all) == 0 {
		return feedbackRow{}, errors.New("no words found in generated output")
	}

	uniqMap := make(map[string]bool, len(all))
	for _, w := range all {
		uniqMap[w] = true
	}
	unique := len(uniqMap)
	dupPct := (1.0 - float64(unique)/float64(len(all))) * 100.0

	socialHits := countTermHits(all, socialTerms)
	observableHits := countTermHits(all, observableTerms)

	meta := parseMetadataFromFilename(path)

	return feedbackRow{
		Timestamp:          time.Now().Format(time.RFC3339),
		File:               path,
		Mode:               meta.Mode,
		InputOrder:         meta.InputOrder,
		SetCount:           meta.SetCount,
		WordsPerSet:        meta.WordsPerSet,
		Tokens:             meta.Tokens,
		Think:              meta.Think,
		Temperature:        meta.Temperature,
		TopP:               meta.TopP,
		UniqueWords:        unique,
		TotalWords:         len(all),
		DuplicatePct:       dupPct,
		SocialTermHits:     socialHits,
		ObservableTermHits: observableHits,
	}, nil
}

func normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Join(strings.Fields(s), " ")
	return s
}

func countTermHits(words []string, terms []string) int {
	hits := 0
	for _, w := range words {
		for _, term := range terms {
			if strings.Contains(w, term) {
				hits++
				break
			}
		}
	}
	return hits
}

func parseMetadataFromFilename(path string) feedbackRow {
	base := filepath.Base(path)
	var r feedbackRow

	// Try new format first: think-{bool}_temp-{float}_topp-{float}_sets-N_words-M
	if m := newFilenamePattern.FindStringSubmatch(base); len(m) == 6 {
		r.Think = m[1]
		r.Temperature = m[2]
		r.TopP = m[3]
		r.SetCount, _ = strconv.Atoi(m[4])
		r.WordsPerSet, _ = strconv.Atoi(m[5])
		return r
	}

	// Fall back to old format: mode-X_order-Y_sets-N_words-M_tokens-T
	if m := oldFilenamePattern.FindStringSubmatch(base); len(m) == 6 {
		r.Mode = m[1]
		r.InputOrder = m[2]
		r.SetCount, _ = strconv.Atoi(m[3])
		r.WordsPerSet, _ = strconv.Atoi(m[4])
		r.Tokens, _ = strconv.Atoi(m[5])
		return r
	}

	return r
}

func userWeightedScore(r feedbackRow) float64 {
	total := r.ObservableScore + r.FunScore + r.SocialScore + r.VarietyScore + r.RelevanceScore
	return float64(total) / 25.0 * 100.0
}

func autoQualityScore(r feedbackRow) float64 {
	diversity := 100.0 - r.DuplicatePct
	socialPenalty := minFloat(40.0, float64(r.SocialTermHits)*2.0)
	observableBonus := minFloat(20.0, float64(r.ObservableTermHits)*0.5)
	auto := diversity - socialPenalty + observableBonus
	if auto < 0 {
		auto = 0
	}
	if auto > 100 {
		auto = 100
	}
	return auto
}

func hybridScore(r feedbackRow) float64 {
	return 0.75*userWeightedScore(r) + 0.25*autoQualityScore(r)
}

func appendFeedback(path string, r feedbackRow) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	needHeader := false
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			needHeader = true
		} else {
			return fmt.Errorf("stat: %w", err)
		}
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open feedback file: %w", err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	if needHeader {
		header := []string{
			"timestamp", "file",
			"mode", "input_order", "tokens",
			"think", "temperature", "top_p",
			"set_count", "words_per_set",
			"unique_words", "total_words", "duplicate_pct", "social_term_hits", "observable_term_hits",
			"observable_score", "fun_score", "social_score", "variety_score", "relevance_score",
			"user_weighted_score", "hybrid_score", "notes",
		}
		if err := w.Write(header); err != nil {
			return fmt.Errorf("write header: %w", err)
		}
	}

	rec := []string{
		r.Timestamp,
		r.File,
		r.Mode,
		r.InputOrder,
		strconv.Itoa(r.Tokens),
		r.Think,
		r.Temperature,
		r.TopP,
		strconv.Itoa(r.SetCount),
		strconv.Itoa(r.WordsPerSet),
		strconv.Itoa(r.UniqueWords),
		strconv.Itoa(r.TotalWords),
		fmt.Sprintf("%.2f", r.DuplicatePct),
		strconv.Itoa(r.SocialTermHits),
		strconv.Itoa(r.ObservableTermHits),
		strconv.Itoa(r.ObservableScore),
		strconv.Itoa(r.FunScore),
		strconv.Itoa(r.SocialScore),
		strconv.Itoa(r.VarietyScore),
		strconv.Itoa(r.RelevanceScore),
		fmt.Sprintf("%.2f", r.UserWeightedScore),
		fmt.Sprintf("%.2f", r.HybridScore),
		r.Notes,
	}
	if err := w.Write(rec); err != nil {
		return fmt.Errorf("write row: %w", err)
	}
	return nil
}

func printSummary(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open feedback file: %w", err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	recs, err := r.ReadAll()
	if err != nil {
		return fmt.Errorf("read feedback csv: %w", err)
	}
	if len(recs) <= 1 {
		return errors.New("no feedback rows found")
	}

	head := recs[0]
	idx := map[string]int{}
	for i, h := range head {
		idx[h] = i
	}

	type agg struct {
		Count      int
		UserSum    float64
		HybridSum  float64
		DupSum     float64
		UniqueSum  float64
		RuntimeSum float64
	}
	byConfig := map[string]*agg{}

	for _, rec := range recs[1:] {
		// Build config key from whichever column set is populated.
		var cfg string
		if i, ok := idx["think"]; ok && i < len(rec) && rec[i] != "" {
			temp := safeIdx(rec, idx, "temperature")
			topp := safeIdx(rec, idx, "top_p")
			cfg = fmt.Sprintf("think=%s/temp=%s/topp=%s", rec[i], temp, topp)
		} else {
			cfg = rec[idx["mode"]] + "/" + rec[idx["input_order"]] + "/t" + rec[idx["tokens"]]
		}
		a := byConfig[cfg]
		if a == nil {
			a = &agg{}
			byConfig[cfg] = a
		}
		a.Count++
		a.UserSum += parseFloat(rec[idx["user_weighted_score"]])
		a.HybridSum += parseFloat(rec[idx["hybrid_score"]])
		a.DupSum += parseFloat(rec[idx["duplicate_pct"]])
		a.UniqueSum += parseFloat(rec[idx["unique_words"]])
	}

	keys := make([]string, 0, len(byConfig))
	for k := range byConfig {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	fmt.Printf("feedback rows: %d\n", len(recs)-1)
	for _, k := range keys {
		a := byConfig[k]
		fmt.Printf("%s | n=%d user=%.1f hybrid=%.1f dup=%.1f unique=%.1f\n",
			k,
			a.Count,
			a.UserSum/float64(a.Count),
			a.HybridSum/float64(a.Count),
			a.DupSum/float64(a.Count),
			a.UniqueSum/float64(a.Count),
		)
	}

	return nil
}

func safeIdx(rec []string, idx map[string]int, col string) string {
	i, ok := idx[col]
	if !ok || i >= len(rec) {
		return ""
	}
	return rec[i]
}

func parseFloat(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
