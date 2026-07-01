package server

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/jkMLnop/binGO-CLI/db"
)

const maxInMemoryFeedbackEntries = 2000

// LLMFeedbackExcludedWord captures one excluded buzzword and why it was rejected.
type LLMFeedbackExcludedWord struct {
	Word            string `json:"word"`
	Reason          string `json:"reason"`
	OtherText       string `json:"other_text,omitempty"`
	DuplicateOf     string `json:"duplicate_of,omitempty"`
	SpecificityNote string `json:"specificity_note,omitempty"`
	RetrievalURL    string `json:"retrieval_url,omitempty"`
}

// LLMFeedbackEntry is the normalized feedback payload sent from the web UI.
type LLMFeedbackEntry struct {
	GameCode       string                    `json:"game_code"`
	Topic          string                    `json:"topic"`
	SourceURL      string                    `json:"url,omitempty"`
	SetLabel       string                    `json:"set_label,omitempty"`
	GenerationMode string                    `json:"generation_mode,omitempty"`
	TotalWords     int                       `json:"total_words"`
	IncludedWords  []string                  `json:"included_words"`
	Excluded       []LLMFeedbackExcludedWord `json:"excluded"`
	SubmittedBy    string                    `json:"submitted_by,omitempty"`
	SubmittedAt    time.Time                 `json:"submitted_at"`
}

func normalizeFeedbackWord(word string) string {
	word = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, word)
	return strings.TrimSpace(word)
}

func normalizeFeedbackTopic(topic string) string {
	return strings.ToLower(strings.TrimSpace(topic))
}

func isAllowedFeedbackReason(reason string) bool {
	switch reason {
	case "", "not_observable", "too_generic", "duplicate", "not_relevant", "too_hard", "safety_accessibility", "other":
		return true
	default:
		return false
	}
}

func (s *Server) storeLLMFeedback(entry LLMFeedbackEntry) {
	if s.DB != nil {
		dbCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		dbEntry := db.LLMFeedbackEntry{
			GameCode:       entry.GameCode,
			Topic:          entry.Topic,
			SourceURL:      entry.SourceURL,
			SetLabel:       entry.SetLabel,
			GenerationMode: normalizeGenerationMode(entry.GenerationMode),
			TotalWords:     entry.TotalWords,
			IncludedWords:  append([]string(nil), entry.IncludedWords...),
			SubmittedBy:    entry.SubmittedBy,
			SubmittedAt:    entry.SubmittedAt,
		}
		dbEntry.Excluded = make([]db.LLMFeedbackExcludedWord, 0, len(entry.Excluded))
		for _, rejected := range entry.Excluded {
			dbEntry.Excluded = append(dbEntry.Excluded, db.LLMFeedbackExcludedWord{
				Word:            rejected.Word,
				Reason:          rejected.Reason,
				OtherText:       rejected.OtherText,
				DuplicateOf:     rejected.DuplicateOf,
				SpecificityNote: rejected.SpecificityNote,
				RetrievalURL:    rejected.RetrievalURL,
			})
		}

		if err := s.DB.SaveLLMFeedback(dbCtx, dbEntry); err != nil {
			log.Printf("Warning: failed to persist LLM feedback to DB: %v", err)
			s.Metrics.RecordError("db")
		}
		return
	}

	s.FeedbackMu.Lock()
	defer s.FeedbackMu.Unlock()

	s.LLMFeedback = append(s.LLMFeedback, entry)
	if len(s.LLMFeedback) > maxInMemoryFeedbackEntries {
		overflow := len(s.LLMFeedback) - maxInMemoryFeedbackEntries
		s.LLMFeedback = append([]LLMFeedbackEntry(nil), s.LLMFeedback[overflow:]...)
	}
}

func (s *Server) llmFeedbackGuidance(gameCode, topic, generationMode string) string {
	entries := make([]LLMFeedbackEntry, 0, 120)

	if s.DB != nil {
		dbCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		dbEntries, err := s.DB.GetRecentLLMFeedback(dbCtx, gameCode, topic, 120)
		if err != nil {
			log.Printf("Warning: failed to load LLM feedback guidance from DB: %v", err)
			s.Metrics.RecordError("db")
		} else {
			entries = make([]LLMFeedbackEntry, 0, len(dbEntries))
			for _, entry := range dbEntries {
				mapped := LLMFeedbackEntry{
					GameCode:       entry.GameCode,
					Topic:          entry.Topic,
					SourceURL:      entry.SourceURL,
					SetLabel:       entry.SetLabel,
					GenerationMode: normalizeGenerationMode(entry.GenerationMode),
					TotalWords:     entry.TotalWords,
					IncludedWords:  append([]string(nil), entry.IncludedWords...),
					SubmittedBy:    entry.SubmittedBy,
					SubmittedAt:    entry.SubmittedAt,
				}
				mapped.Excluded = make([]LLMFeedbackExcludedWord, 0, len(entry.Excluded))
				for _, rejected := range entry.Excluded {
					mapped.Excluded = append(mapped.Excluded, LLMFeedbackExcludedWord{
						Word:            rejected.Word,
						Reason:          rejected.Reason,
						OtherText:       rejected.OtherText,
						DuplicateOf:     rejected.DuplicateOf,
						SpecificityNote: rejected.SpecificityNote,
						RetrievalURL:    rejected.RetrievalURL,
					})
				}
				entries = append(entries, mapped)
			}
		}
	} else {
		s.FeedbackMu.RLock()
		entries = append(entries, s.LLMFeedback...)
		s.FeedbackMu.RUnlock()
	}

	if len(entries) == 0 {
		return ""
	}

	normalizedTopic := normalizeFeedbackTopic(topic)
	normalizedMode := normalizeGenerationMode(generationMode)
	const sameModeWeightBonus = 2
	reasonCounts := map[string]int{}
	excludedCounts := map[string]int{}
	includedCounts := map[string]int{}
	tooGenericNotes := map[string]int{}
	retrievalURLCounts := map[string]int{}
	matched := 0

	for _, entry := range entries {
		if gameCode != "" && entry.GameCode != "" && !strings.EqualFold(entry.GameCode, gameCode) {
			continue
		}
		entryTopic := normalizeFeedbackTopic(entry.Topic)
		if normalizedTopic != "" && entryTopic != "" && normalizedTopic != entryTopic {
			continue
		}
		matched++
		weight := 1
		if normalizedMode != "" && normalizeGenerationMode(entry.GenerationMode) == normalizedMode {
			weight += sameModeWeightBonus
		}

		for _, word := range entry.IncludedWords {
			clean := normalizeFeedbackWord(word)
			if clean == "" {
				continue
			}
			includedCounts[strings.ToLower(clean)] += weight
		}
		for _, rejected := range entry.Excluded {
			cleanWord := normalizeFeedbackWord(rejected.Word)
			if cleanWord != "" {
				excludedCounts[strings.ToLower(cleanWord)] += weight
			}
			reason := strings.TrimSpace(rejected.Reason)
			if reason != "" {
				reasonCounts[reason] += weight
			}
			if reason == "too_generic" {
				note := normalizeFeedbackWord(rejected.SpecificityNote)
				if note != "" {
					tooGenericNotes[strings.ToLower(note)] += weight
				}
				if rejected.RetrievalURL != "" {
					retrievalURLCounts[strings.TrimSpace(rejected.RetrievalURL)] += weight
				}
			}
		}
	}

	if matched == 0 {
		return ""
	}

	frequentExcluded := topCountPairs(excludedCounts, 8)
	frequentReasons := topCountPairs(reasonCounts, 4)
	frequentIncluded := topCountPairs(includedCounts, 8)
	frequentTooGenericNotes := topCountPairs(tooGenericNotes, 5)
	frequentRetrievalURLs := topCountPairs(retrievalURLCounts, 3)

	if len(frequentExcluded) == 0 && len(frequentReasons) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("User scoring guidance from prior rounds:\n")
	if len(frequentExcluded) > 0 {
		b.WriteString("- Avoid words that have been repeatedly excluded: ")
		b.WriteString(strings.Join(formatCountPairs(frequentExcluded), ", "))
		b.WriteString(".\n")
	}
	if len(frequentReasons) > 0 {
		b.WriteString("- Most common exclusion reasons: ")
		b.WriteString(strings.Join(formatCountPairs(frequentReasons), ", "))
		b.WriteString(".\n")
	}
	if len(frequentIncluded) > 0 {
		b.WriteString("- Prefer concrete items similar to accepted words like: ")
		b.WriteString(strings.Join(formatCountPairs(frequentIncluded), ", "))
		b.WriteString(".\n")
	}
	if len(frequentTooGenericNotes) > 0 {
		b.WriteString("- Too generic items should be replaced with specific sightings such as: ")
		b.WriteString(strings.Join(formatCountPairs(frequentTooGenericNotes), ", "))
		b.WriteString(".\n")
	}
	if len(frequentRetrievalURLs) > 0 {
		b.WriteString("- Use these pages for concrete details when available: ")
		b.WriteString(strings.Join(formatCountPairs(frequentRetrievalURLs), ", "))
		b.WriteString(".\n")
	}
	b.WriteString("Prioritize short, observable, in-person sightings over abstract ideas.")
	return b.String()
}

type countPair struct {
	Label string
	Count int
}

func topCountPairs(values map[string]int, limit int) []countPair {
	pairs := make([]countPair, 0, len(values))
	for label, count := range values {
		if label == "" || count <= 0 {
			continue
		}
		pairs = append(pairs, countPair{Label: label, Count: count})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].Count == pairs[j].Count {
			return pairs[i].Label < pairs[j].Label
		}
		return pairs[i].Count > pairs[j].Count
	})
	if len(pairs) > limit {
		pairs = pairs[:limit]
	}
	return pairs
}

func formatCountPairs(pairs []countPair) []string {
	formatted := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		formatted = append(formatted, fmt.Sprintf("%s (%d)", pair.Label, pair.Count))
	}
	return formatted
}
