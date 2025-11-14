package shared

import (
	"encoding/csv"
	"log"
	"os"
)

// LoadBuzzwords reads buzzwords from a CSV file and returns them as a slice of strings
func LoadBuzzwords(filename string) [][]string {
	file, err := os.Open(filename)
	if err != nil {
		log.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	rows, err := reader.ReadAll()
	if err != nil {
		log.Fatalf("Failed to read CSV file: %v", err)
	}

	if len(rows) < 9 {
		log.Fatalf("Not enough rows in the CSV file to populate a 3x3 matrix")
	}

	return rows
}
