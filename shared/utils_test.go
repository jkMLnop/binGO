package shared

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadBuzzwords tests loading buzzwords from a CSV file
func TestLoadBuzzwords(t *testing.T) {
	testCases := []struct {
		name        string
		filename    string
		setup       func(t *testing.T) string // returns path to test file
		expectError bool
		checkRows   func(t *testing.T, rows [][]string)
	}{
		{
			name:     "valid buzzwords file",
			filename: "test_buzzwords.csv",
			setup: func(t *testing.T) string {
				tmpFile, err := os.CreateTemp("", "buzzwords_*.csv")
				if err != nil {
					t.Fatalf("Failed to create temp file: %v", err)
				}
				defer tmpFile.Close()

				// Write 10 rows of buzzwords
				content := "buzzword1\nbuzzword2\nbuzzword3\nbuzzword4\nbuzzword5\nbuzzword6\nbuzzword7\nbuzzword8\nbuzzword9\nbuzzword10\n"
				if _, err := tmpFile.WriteString(content); err != nil {
					t.Fatalf("Failed to write temp file: %v", err)
				}
				return tmpFile.Name()
			},
			expectError: false,
			checkRows: func(t *testing.T, rows [][]string) {
				if len(rows) != 10 {
					t.Errorf("Expected 10 rows, got %d", len(rows))
				}
			},
		},
		{
			name:     "insufficient rows (8 rows, needs 9)",
			filename: "test_insufficient.csv",
			setup: func(t *testing.T) string {
				tmpFile, err := os.CreateTemp("", "buzzwords_*.csv")
				if err != nil {
					t.Fatalf("Failed to create temp file: %v", err)
				}
				defer tmpFile.Close()

				// Write only 8 rows
				content := "buzzword1\nbuzzword2\nbuzzword3\nbuzzword4\nbuzzword5\nbuzzword6\nbuzzword7\nbuzzword8\n"
				if _, err := tmpFile.WriteString(content); err != nil {
					t.Fatalf("Failed to write temp file: %v", err)
				}
				return tmpFile.Name()
			},
			expectError: true,
			checkRows:   nil,
		},
		{
			name:     "file not found",
			filename: "/nonexistent/path/buzzwords.csv",
			setup: func(t *testing.T) string {
				return "/nonexistent/path/buzzwords.csv"
			},
			expectError: true,
			checkRows:   nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			filePath := tc.setup(t)
			defer func() {
				// Clean up temp file if it exists
				if _, err := os.Stat(filePath); err == nil {
					os.Remove(filePath)
				}
			}()

			rows, err := LoadBuzzwords(filePath)

			if tc.expectError && err == nil {
				t.Errorf("Expected error, but got nil")
			}
			if !tc.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if !tc.expectError && tc.checkRows != nil {
				tc.checkRows(t, rows)
			}
		})
	}
}

// TestLoadBuzzwordsWithFallback tests fallback behavior
func TestLoadBuzzwordsWithFallback(t *testing.T) {
	testCases := []struct {
		name           string
		fullExists     bool
		standardExists bool
		expectError    bool
	}{
		{
			name:           "full file exists",
			fullExists:     true,
			standardExists: false,
			expectError:    false,
		},
		{
			name:           "only standard file exists",
			fullExists:     false,
			standardExists: true,
			expectError:    false,
		},
		{
			name:           "both files exist (should use full)",
			fullExists:     true,
			standardExists: true,
			expectError:    false,
		},
		{
			name:           "neither file exists",
			fullExists:     false,
			standardExists: false,
			expectError:    true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create temp directory
			tmpDir, err := os.MkdirTemp("", "buzzwords_test_*")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tmpDir)

			// Change to temp directory
			oldCwd, err := os.Getwd()
			if err != nil {
				t.Fatalf("Failed to get current dir: %v", err)
			}
			if err := os.Chdir(tmpDir); err != nil {
				t.Fatalf("Failed to change dir: %v", err)
			}
			defer os.Chdir(oldCwd)

			// Create test files as needed
			if tc.fullExists {
				fullPath := filepath.Join(tmpDir, "buzzwords_full.csv")
				content := "b1\nb2\nb3\nb4\nb5\nb6\nb7\nb8\nb9\nb10\n"
				if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
					t.Fatalf("Failed to create full file: %v", err)
				}
			}

			if tc.standardExists {
				standardPath := filepath.Join(tmpDir, "buzzwords.csv")
				content := "s1\ns2\ns3\ns4\ns5\ns6\ns7\ns8\ns9\ns10\n"
				if err := os.WriteFile(standardPath, []byte(content), 0644); err != nil {
					t.Fatalf("Failed to create standard file: %v", err)
				}
			}

			// Test the function
			rows, err := LoadBuzzwordsWithFallback()

			if tc.expectError && err == nil {
				t.Errorf("Expected error, but got nil")
			}
			if !tc.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if !tc.expectError {
				if len(rows) == 0 {
					t.Errorf("Expected non-empty rows")
				}
				// Verify which file was used
				if tc.fullExists {
					if len(rows) > 0 && rows[0][0] != "b1" {
						t.Errorf("Expected full file to be used, but got %s", rows[0][0])
					}
				} else if tc.standardExists {
					if len(rows) > 0 && rows[0][0] != "s1" {
						t.Errorf("Expected standard file to be used, but got %s", rows[0][0])
					}
				}
			}
		})
	}
}

// TestLoadBuzzwordsEmptyFile tests loading an empty or nearly-empty file
func TestLoadBuzzwordsEmptyFile(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "buzzwords_*.csv")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	// File is empty - should fail with "not enough rows"
	rows, err := LoadBuzzwords(tmpFile.Name())

	if err == nil {
		t.Errorf("Expected error for empty file, but got nil")
	}
	if rows != nil {
		t.Errorf("Expected nil rows for error case, but got %v", rows)
	}
}
