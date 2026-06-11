package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/duynguyendang/manglekit/adapters/knowledge"
	"github.com/duynguyendang/manglekit/sdk"
)

func exampleRoot() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Dir(filename)
}

func loadTestClient(t *testing.T) *sdk.Client {
	t.Helper()
	ctx := context.Background()

	client, err := sdk.NewClient(ctx)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	t.Cleanup(func() { client.Shutdown(ctx) })

	ntPath := filepath.Join(exampleRoot(), "ontology.nt")
	data, err := os.ReadFile(ntPath)
	if err != nil {
		t.Fatalf("Failed to read ontology.nt: %v", err)
	}
	facts, err := knowledge.ParseNTriples(strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("Failed to parse N-Triples: %v", err)
	}
	if err := client.LoadFacts(facts); err != nil {
		t.Fatalf("Failed to load facts: %v", err)
	}

	policy := `
		manages_star(X, Y) :- triple(X, "reports_to", Y).
		manages_star(X, Y) :- triple(X, "reports_to", Z), manages_star(Z, Y).
		can_access_doc(User, Doc) :- triple(Team, "has_member", User), triple(Project, "owned_by", Team), triple(Project, "has_doc", Doc).
	`
	if err := client.Engine().LoadPolicy(ctx, policy); err != nil {
		t.Fatalf("Failed to load policy: %v", err)
	}

	return client
}

func TestTransitiveReportsTo(t *testing.T) {
	client := loadTestClient(t)
	ctx := context.Background()

	tests := []struct {
		name     string
		query    string
		expected []string
	}{
		{
			name:     "alice full chain",
			query:    `manages_star("alice", Y)`,
			expected: []string{"bob", "carol", "dave", "ceo"},
		},
		{
			name:     "bob reports to carol, dave, ceo",
			query:    `manages_star("bob", Y)`,
			expected: []string{"carol", "dave", "ceo"},
		},
		{
			name:     "carol reports to dave and ceo",
			query:    `manages_star("carol", Y)`,
			expected: []string{"dave", "ceo"},
		},
		{
			name:     "dave reports to ceo only",
			query:    `manages_star("dave", Y)`,
			expected: []string{"ceo"},
		},
		{
			name:     "ceo has no manager",
			query:    `manages_star("ceo", Y)`,
			expected: nil,
		},
		{
			name:     "who reports to carol transitively",
			query:    `manages_star(X, "carol")`,
			expected: []string{"alice", "bob"},
		},
		{
			name:     "who reports to ceo transitively",
			query:    `manages_star(X, "ceo")`,
			expected: []string{"alice", "bob", "carol", "dave"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			solutions, err := client.Engine().Query(ctx, nil, tt.query)
			if err != nil {
				t.Fatalf("Query failed: %v", err)
			}

			var got []string
			for _, s := range solutions {
				for _, v := range s {
					got = append(got, v)
				}
			}

			if len(got) != len(tt.expected) {
				t.Errorf("expected %d results, got %d: %v", len(tt.expected), len(got), got)
				return
			}

			gotSet := make(map[string]bool)
			for _, v := range got {
				gotSet[v] = true
			}
			for _, want := range tt.expected {
				if !gotSet[want] {
					t.Errorf("expected %q in results, not found in %v", want, got)
				}
			}
		})
	}
}

func TestTeamAccessControl(t *testing.T) {
	client := loadTestClient(t)
	ctx := context.Background()

	tests := []struct {
		name       string
		user       string
		doc        string
		expectAccess bool
	}{
		{
			name:       "alice can access spec_alpha via team_platform",
			user:       "alice",
			doc:        "spec_alpha",
			expectAccess: true,
		},
		{
			name:       "bob can access spec_alpha via team_platform",
			user:       "bob",
			doc:        "spec_alpha",
			expectAccess: true,
		},
		{
			name:       "eve cannot access spec_alpha (team_infra, not team_platform)",
			user:       "eve",
			doc:        "spec_alpha",
			expectAccess: false,
		},
		{
			name:       "eve can access spec_beta via team_infra",
			user:       "eve",
			doc:        "spec_beta",
			expectAccess: true,
		},
		{
			name:       "alice cannot access spec_beta (team_platform, not team_infra)",
			user:       "alice",
			doc:        "spec_beta",
			expectAccess: false,
		},
		{
			name:       "carol has no team membership, cannot access any doc",
			user:       "carol",
			doc:        "spec_alpha",
			expectAccess: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query := `can_access_doc("` + tt.user + `", "` + tt.doc + `")`
			solutions, err := client.Engine().Query(ctx, nil, query)
			if err != nil {
				t.Fatalf("Query failed: %v", err)
			}

			hasAccess := len(solutions) > 0
			if hasAccess != tt.expectAccess {
				if tt.expectAccess {
					t.Errorf("expected %s to access %s, but access was denied", tt.user, tt.doc)
				} else {
					t.Errorf("expected %s to be denied access to %s, but access was granted", tt.user, tt.doc)
				}
			}
		})
	}
}
