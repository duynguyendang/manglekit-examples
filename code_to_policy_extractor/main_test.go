package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/sdk"
)

func TestGetLayer(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"controllers/auth_controller.go", "controllers/"},
		{"usecases/auth_usecase.go", "usecases/"},
		{"domain/user.go", "domain/"},
		{"gateways/jwt_gateway.go", "gateways/"},
		{"unknown/file.go", "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := getLayer(tt.path); got != tt.want {
				t.Errorf("getLayer(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestHasValidName(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"controllers/auth_controller.go", true},
		{"controllers/auth.go", false},
		{"usecases/create_usecase.go", true},
		{"domain/user.go", true},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := hasValidName(tt.path); got != tt.want {
				t.Errorf("hasValidName(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestGetSuffix(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"controllers/auth_controller.go", "_controller.go"},
		{"usecases/auth_usecase.go", "_usecase.go"},
		{"domain/user.go", ""},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := getSuffix(tt.path); got != tt.want {
				t.Errorf("getSuffix(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func buildFacts(pr PullRequest) []string {
	var facts []string
	for _, file := range pr.Files {
		facts = append(facts, fmt.Sprintf(`file_path("%s", "%s")`, file.Path, getLayer(file.Path)))
		for _, imp := range file.Imports {
			facts = append(facts, fmt.Sprintf(`file_imports("%s", "%s")`, file.Path, getLayer(imp)))
		}
		if hasValidName(file.Path) {
			facts = append(facts, fmt.Sprintf(`file_name_matches("%s", "%s")`, file.Path, getSuffix(file.Path)))
		}
	}
	return facts
}

func TestPolicyEngine_PassingPR(t *testing.T) {
	ctx := context.Background()

	client, err := sdk.NewClient(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize client: %v", err)
	}
	defer client.Shutdown(ctx)

	if err := client.Engine().LoadPolicy(ctx, archPolicy); err != nil {
		t.Fatalf("Failed to load policy: %v", err)
	}

	passingPR := PullRequest{
		PRID:   "PR-1001",
		Title:  "Add auth feature (clean)",
		Author: "developer1",
		Files: []PRFile{
			{
				Path:    "controllers/auth_controller.go",
				Imports: []string{"usecases/auth_usecase"},
			},
			{
				Path:    "usecases/auth_usecase.go",
				Imports: []string{"domain/user"},
			},
			{
				Path:    "domain/user.go",
				Imports: []string{},
			},
		},
	}

	facts := buildFacts(passingPR)
	if err := client.LoadFacts(facts); err != nil {
		t.Fatalf("Failed to load facts: %v", err)
	}

	env := core.NewEnvelope(passingPR)
	err = client.Engine().Assess(ctx, core.ActionMetadata{Name: "review_pr"}, env)
	if core.IsAlignmentError(err) {
		t.Errorf("Expected passing PR to be approved, but got violation: %v", err)
	}
}

func TestPolicyEngine_ViolatingPR(t *testing.T) {
	ctx := context.Background()

	client, err := sdk.NewClient(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize client: %v", err)
	}
	defer client.Shutdown(ctx)

	if err := client.Engine().LoadPolicy(ctx, archPolicy); err != nil {
		t.Fatalf("Failed to load policy: %v", err)
	}

	violatingPR := PullRequest{
		PRID:   "PR-9999",
		Title:  "Add quick feature (violates architecture)",
		Author: "developer2",
		Files: []PRFile{
			{
				Path:    "controllers/order_controller.go",
				Imports: []string{"usecases/order_usecase", "domain/order"},
			},
			{
				Path:    "usecases/order_usecase.go",
				Imports: []string{"domain/order"},
			},
		},
	}

	facts := buildFacts(violatingPR)
	if err := client.LoadFacts(facts); err != nil {
		t.Fatalf("Failed to load facts: %v", err)
	}

	env := core.NewEnvelope(violatingPR)
	err = client.Engine().Assess(ctx, core.ActionMetadata{Name: "review_pr"}, env)
	if !core.IsAlignmentError(err) {
		t.Errorf("Expected violating PR to be blocked, but got: %v", err)
	}
}
