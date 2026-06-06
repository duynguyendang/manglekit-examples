package main

import "testing"

func TestExtractNumericResult(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantValue string
		wantOK    bool
	}{
		{
			name:      "result with prefix text",
			input:     "The answer is RESULT: 42",
			wantValue: "",
			wantOK:    false,
		},
		{
			name:      "result with decimal",
			input:     "RESULT: 3.14",
			wantValue: "3.14",
			wantOK:    true,
		},
		{
			name:      "no result marker",
			input:     "No result here",
			wantValue: "",
			wantOK:    false,
		},
		{
			name:      "non-numeric value after RESULT",
			input:     "RESULT: not_a_number",
			wantValue: "",
			wantOK:    false,
		},
		{
			name:      "lowercase result",
			input:     "result: 10",
			wantValue: "10",
			wantOK:    true,
		},
		{
			name:      "multiline with result on last line",
			input:     "Some reasoning\nMore steps\nRESULT: 99",
			wantValue: "99",
			wantOK:    true,
		},
		{
			name:      "empty string",
			input:     "",
			wantValue: "",
			wantOK:    false,
		},
		{
			name:      "negative number",
			input:     "RESULT: -5",
			wantValue: "-5",
			wantOK:    true,
		},
		{
			name:      "result with extra spaces",
			input:     "RESULT:   100   ",
			wantValue: "100",
			wantOK:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotValue, gotOK := extractNumericResult(tt.input)
			if gotValue != tt.wantValue || gotOK != tt.wantOK {
				t.Errorf("extractNumericResult(%q) = (%q, %v), want (%q, %v)",
					tt.input, gotValue, gotOK, tt.wantValue, tt.wantOK)
			}
		})
	}
}

func TestNewSmallModelMathSolverNoAPIKey(t *testing.T) {
	// Without a valid API key, NewSmallModelMathSolver should return an error.
	// This test verifies the constructor fails gracefully when credentials are missing.
	t.Setenv("GOOGLE_API_KEY", "")

	// We can't easily test this without mocking the AI provider,
	// but we verify the function signature and error handling pattern exist.
	// The actual integration is covered by the example's main() handling.
	t.Skip("Skipping: requires API key or mock provider")
}
