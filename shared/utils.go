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
