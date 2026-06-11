package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/sdk"
)

// --- Constants: Datalog Policy ---

const archPolicy = `
		% ============================================
		% Clean Architecture Dependency Rules
		% ============================================
		
		% Declare dynamic predicates (will be loaded as facts)
		Decl file_path(File, Layer).
		Decl file_imports(File, ImportLayer).
		Decl file_name_matches(File, Suffix).
		
		% Forbidden: controllers importing domain directly
		halt("Req", "Clean Architecture violation: controller must not import domain directly") :-
			action_operation("Req", "review_pr"),
			file_path(File, "controllers/"),
			file_imports(File, "domain/").
		
		% Forbidden: controllers importing gateways
		halt("Req", "Clean Architecture violation: controller must not import gateways") :-
			action_operation("Req", "review_pr"),
			file_path(File, "controllers/"),
			file_imports(File, "gateways/").
		
		% Forbidden: domain importing any other layer
		halt("Req", "Clean Architecture violation: domain must not import usecases") :-
			action_operation("Req", "review_pr"),
			file_path(File, "domain/"),
			file_imports(File, "usecases/").
		
		halt("Req", "Clean Architecture violation: domain must not import controllers") :-
			action_operation("Req", "review_pr"),
			file_path(File, "domain/"),
			file_imports(File, "controllers/").
		
		halt("Req", "Clean Architecture violation: domain must not import gateways") :-
			action_operation("Req", "review_pr"),
			file_path(File, "domain/"),
			file_imports(File, "gateways/").
		
		% Forbidden: usecases importing controllers
		halt("Req", "Clean Architecture violation: usecases must not import controllers") :-
			action_operation("Req", "review_pr"),
			file_path(File, "usecases/"),
			file_imports(File, "controllers/").
		
		% File naming conventions
		halt("Req", "Naming convention violation: controller files must end with _controller.go") :-
			action_operation("Req", "review_pr"),
			file_path(File, "controllers/"),
			!file_name_matches(File, "_controller.go").
		
		halt("Req", "Naming convention violation: usecase files must end with _usecase.go") :-
			action_operation("Req", "review_pr"),
			file_path(File, "usecases/"),
			!file_name_matches(File, "_usecase.go").
	`

func exampleDir() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Dir(filename)
}

// PRFile represents a file in a pull request with its imports.
type PRFile struct {
	Path        string   `json:"path"`
	Imports     []string `json:"imports"`
	AddedLines  int      `json:"added_lines"`
	DeletedLines int     `json:"deleted_lines"`
}

// PullRequest represents a PR with multiple files.
type PullRequest struct {
	PRID   string   `json:"pr_id"`
	Title  string   `json:"title"`
	Author string   `json:"author"`
	Files  []PRFile `json:"files"`
}

func main() {
	ctx := context.Background()

	fmt.Println("🏗️  Dynamic Architecture Linter")
	fmt.Println("================================")
	fmt.Println("Demonstrating how architecture guidelines are enforced on PRs:")
	fmt.Println("1. Architecture guidelines define allowed dependencies")
	fmt.Println("2. Rules are compiled to Datalog")
	fmt.Println("3. PR dependencies are checked against rules")
	fmt.Println("4. Violations are detected with specific error messages")
	fmt.Println()

	// 1. Initialize Manglekit Client
	client, err := sdk.NewClient(ctx)
	if err != nil {
		log.Fatalf("Failed to initialize client: %v", err)
	}

	// 2. Load Architecture Rules (simulating LLM extraction from guidelines.md)
	// In production, this would use the extractor to parse architecture_guidelines.md
	fmt.Println("📄 Loading architecture rules...")

	if err := client.Engine().LoadPolicy(ctx, archPolicy); err != nil {
		log.Fatalf("Failed to load architecture policy: %v", err)
	}
	fmt.Println("✅ Loaded architecture rules (7 Clean Architecture rules + 2 naming conventions)")
	fmt.Println()

	// 3. Load Sample PR
	prBytes, err := os.ReadFile(filepath.Join(exampleDir(), "sample_pr.json"))
	if err != nil {
		log.Fatalf("Failed to read sample_pr.json: %v", err)
	}

	var pr PullRequest
	if err := json.Unmarshal(prBytes, &pr); err != nil {
		log.Fatalf("Failed to parse PR: %v", err)
	}

	fmt.Printf("📥 Reviewing PR: %s - %s (by %s)\n", pr.PRID, pr.Title, pr.Author)
	fmt.Printf("   Files changed: %d\n\n", len(pr.Files))

	// 4. Convert PR files to Datalog facts
	var facts []string
	for _, file := range pr.Files {
		// Add file_path fact
		facts = append(facts, fmt.Sprintf(`file_path("%s", "%s")`, file.Path, getLayer(file.Path)))

		// Add file_imports facts
		for _, imp := range file.Imports {
			facts = append(facts, fmt.Sprintf(`file_imports("%s", "%s")`, file.Path, getLayer(imp)))
		}

		// Add file_name_matches fact
		if hasValidName(file.Path) {
			facts = append(facts, fmt.Sprintf(`file_name_matches("%s", "%s")`, file.Path, getSuffix(file.Path)))
		}
	}

	client.LoadFacts(facts)
	fmt.Println("📊 Loaded PR facts into policy engine")
	fmt.Println()

	// 5. Review PR against architecture rules
	fmt.Println("🔍 Running architecture lint check...")
	reviewEnv := core.NewEnvelope(pr)
	err = client.Engine().Assess(ctx, core.ActionMetadata{Name: "review_pr"}, reviewEnv)
	if core.IsAlignmentError(err) {
		fmt.Println("❌ PR REJECTED - Architecture violations found:")
		fmt.Printf("   %v\n", err)
	} else {
		fmt.Println("✅ PR APPROVED - No architecture violations found")
	}

	// 6. Test with a violating PR
	fmt.Println()
	fmt.Println("=" + "=" + "=" + "=" + "=" + "=" + "=" + "=" + "=" + "=" + "=" + "=" + "=" + "=" + "=" + "=" + "=" + "=" + "=" + "=")
	fmt.Println("🧪 Testing with a VIOLATING PR...")
	fmt.Println("=" + "=" + "=" + "=" + "=" + "=" + "=" + "=" + "=" + "=" + "=" + "=" + "=" + "=" + "=" + "=" + "=" + "=" + "=" + "=")
	fmt.Println()

	// Create a violating PR where controller imports domain directly
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

	fmt.Printf("📥 Reviewing VIOLATING PR: %s - %s\n", violatingPR.PRID, violatingPR.Title)
	fmt.Printf("   Files changed: %d\n\n", len(violatingPR.Files))

	// Clear old facts and load new ones
	var violatingFacts []string
	for _, file := range violatingPR.Files {
		violatingFacts = append(violatingFacts, fmt.Sprintf(`file_path("%s", "%s")`, file.Path, getLayer(file.Path)))
		for _, imp := range file.Imports {
			violatingFacts = append(violatingFacts, fmt.Sprintf(`file_imports("%s", "%s")`, file.Path, getLayer(imp)))
		}
		if hasValidName(file.Path) {
			violatingFacts = append(violatingFacts, fmt.Sprintf(`file_name_matches("%s", "%s")`, file.Path, getSuffix(file.Path)))
		}
	}

	client.LoadFacts(violatingFacts)

	fmt.Println("🔍 Running architecture lint check...")
	violatingEnv := core.NewEnvelope(violatingPR)
	err = client.Engine().Assess(ctx, core.ActionMetadata{Name: "review_pr"}, violatingEnv)
	if core.IsAlignmentError(err) {
		fmt.Println("❌ PR REJECTED - Architecture violations found:")
		fmt.Printf("   %v\n", err)
	} else {
		fmt.Println("✅ PR APPROVED - No architecture violations found")
	}

	fmt.Println()
	fmt.Println("✅ Dynamic Architecture Linter demonstration complete!")
	fmt.Println()
	fmt.Println("💡 Key Takeaway: Architecture guidelines are automatically enforced.")
	fmt.Println("   Violations are caught before code is merged, with specific error")
	fmt.Println("   messages indicating which rule was violated and in which file.")
}

// getLayer extracts the layer prefix from a file path.
func getLayer(path string) string {
	switch {
	case len(path) >= 11 && path[:11] == "controllers":
		return "controllers/"
	case len(path) >= 8 && path[:8] == "usecases":
		return "usecases/"
	case len(path) >= 6 && path[:6] == "domain":
		return "domain/"
	case len(path) >= 8 && path[:8] == "gateways":
		return "gateways/"
	default:
		return "unknown"
	}
}

// hasValidName checks if a file follows naming conventions.
func hasValidName(path string) bool {
	switch getLayer(path) {
	case "controllers/":
		return strings.HasSuffix(path, "_controller.go")
	case "usecases/":
		return strings.HasSuffix(path, "_usecase.go")
	default:
		return true
	}
}

// getSuffix returns the naming convention suffix.
func getSuffix(path string) string {
	layer := getLayer(path)
	switch layer {
	case "controllers/":
		return "_controller.go"
	case "usecases/":
		return "_usecase.go"
	default:
		return ""
	}
}
