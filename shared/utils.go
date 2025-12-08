package shared

import (
	"encoding/csv"
	"fmt"
	"os"
)

// LoadBuzzwords reads buzzwords from a CSV file and returns them as a slice of strings
// Returns error if file cannot be opened, parsed, or contains insufficient rows
func LoadBuzzwords(filename string) ([][]string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	if len(rows) < 9 {
		return nil, fmt.Errorf("not enough rows in CSV file: have %d, need at least 9", len(rows))
	}

	return rows, nil
}

// LoadBuzzwordsWithFallback tries to load buzzwords_full.csv first,
// then falls back to buzzwords.csv if full version doesn't exist
func LoadBuzzwordsWithFallback() ([][]string, error) {
	// Try full buzzwords first
	buzzwords, err := LoadBuzzwords("buzzwords_full.csv")
	if err == nil {
		return buzzwords, nil
	}

	// Fall back to standard buzzwords
	buzzwords, err = LoadBuzzwords("buzzwords.csv")
	if err != nil {
		return nil, fmt.Errorf("could not load buzzwords: buzzwords_full.csv not found, buzzwords.csv error: %w", err)
	}

	return buzzwords, nil
}
