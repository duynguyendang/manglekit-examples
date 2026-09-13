// knowledge_graph_reasoning answers: Can org-graph questions be answered with policy-proof lineage? (UC-K1)

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"github.com/duynguyendang/manglekit/adapters/knowledge"
	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/sdk"
)

// --- Constants: Datalog Policy ---

const knowledgePolicy = `
		manages_star(X, Y) :- triple(X, "reports_to", Y).
		manages_star(X, Y) :- triple(X, "reports_to", Z), manages_star(Z, Y).
		can_access_doc(User, Doc) :- triple(Team, "has_member", User), triple(Project, "owned_by", Team), triple(Project, "has_doc", Doc).
	`

func exampleDir() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Dir(filename)
}

func main() {
	ctx := context.Background()

	fmt.Println("Knowledge Graph Reasoning with Manglekit")
	fmt.Println("=========================================")
	fmt.Println()

	client, err := sdk.NewClient(ctx)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	ntPath := filepath.Join(exampleDir(), "ontology.nt")
	f, err := os.Open(ntPath)
	if err != nil {
		log.Fatalf("Failed to open ontology.nt: %v", err)
	}
	facts, err := knowledge.ParseNTriples(f)
	f.Close()
	if err != nil {
		log.Fatalf("Failed to parse N-Triples: %v", err)
	}

	if err := client.LoadFacts(ctx, facts); err != nil {
		log.Fatalf("Failed to load facts: %v", err)
	}
	fmt.Printf("Loaded %d facts from ontology.nt\n\n", len(facts))

	if err := client.Engine().LoadPolicy(ctx, knowledgePolicy); err != nil {
		log.Fatalf("Failed to load policy: %v", err)
	}

	fmt.Println("--- Query 1: Direct reports of bob ---")
	solutions, err := client.Engine().Query(ctx, nil, `triple(X, "reports_to", "bob")`)
	if err != nil {
		log.Fatalf("Query failed: %v", err)
	}
	for _, s := range solutions {
		fmt.Printf("  %s reports to bob\n", s["X"])
	}
	fmt.Println()

	fmt.Println("--- Query 2: Who does alice transitively report to? ---")
	solutions, err = client.Engine().Query(ctx, nil, `manages_star("alice", Y)`)
	if err != nil {
		log.Fatalf("Query failed: %v", err)
	}
	for _, s := range solutions {
		fmt.Printf("  alice -> %s\n", s["Y"])
	}
	fmt.Println()

	fmt.Println("--- Query 3: Can alice access spec_alpha? ---")
	solutions, err = client.Engine().Query(ctx, nil, `can_access_doc("alice", "spec_alpha")`)
	if err != nil {
		log.Fatalf("Query failed: %v", err)
	}
	if len(solutions) > 0 {
		fmt.Println("  YES: alice can access spec_alpha (member of team_platform)")
	} else {
		fmt.Println("  NO: alice cannot access spec_alpha")
	}
	fmt.Println()

	fmt.Println("--- Query 4: Can eve access spec_alpha? ---")
	solutions, err = client.Engine().Query(ctx, nil, `can_access_doc("eve", "spec_alpha")`)
	if err != nil {
		log.Fatalf("Query failed: %v", err)
	}
	if len(solutions) > 0 {
		fmt.Println("  YES: eve can access spec_alpha")
	} else {
		fmt.Println("  NO: eve cannot access spec_alpha (member of team_infra, not team_platform)")
	}
	fmt.Println()

	fmt.Println("--- Query 5: Can eve access spec_beta? ---")
	solutions, err = client.Engine().Query(ctx, nil, `can_access_doc("eve", "spec_beta")`)
	if err != nil {
		log.Fatalf("Query failed: %v", err)
	}
	if len(solutions) > 0 {
		fmt.Println("  YES: eve can access spec_beta (member of team_infra)")
	} else {
		fmt.Println("  NO: eve cannot access spec_beta")
	}
	fmt.Println()

	fmt.Println("--- Query 6: Temporary facts via Query ---")
	solutions, err = client.Engine().Query(ctx, []string{
		`triple("frank", "reports_to", "alice")`,
	}, `triple("frank", "reports_to", "alice")`)
	if err != nil {
		log.Fatalf("Query failed: %v", err)
	}
	if len(solutions) > 0 {
		fmt.Println("  Confirmed: frank reports_to alice (temporary fact)")
	}
	fmt.Println()

	fmt.Println("--- Query 7: QueryWithAudit (audit trail) ---")
	runQueryWithAudit(ctx, client, facts)
	fmt.Println()

	fmt.Println("Done.")
}

func runQueryWithAudit(ctx context.Context, client *sdk.Client, facts []string) {
	// Show the audit trail through the existing client instead of spinning
	// up a second engine. AssessPlan evaluates the policy and returns a
	// Decision; for a read-only policy with no halt rules the AuditTrail is
	// empty (it is only populated when a halt rule fires), so we also print
	// the derived solutions as the concrete proof of what the policy computed.
	env := core.NewEnvelope("query: who does alice report to")
	for _, f := range facts {
		env.Facts = append(env.Facts, f)
	}
	env.Facts = append(env.Facts, `action_operation("Req", "query_kb").`)

	decision, err := client.Engine().AssessPlan(ctx, env)
	if err != nil {
		log.Fatalf("AssessPlan failed: %v", err)
	}

	fmt.Printf("  Outcome: %s\n", decision.Outcome)
	if decision.AuditTrail != nil && len(decision.AuditTrail.MatchedRules) > 0 {
		fmt.Printf("  Matched: %d rules\n", len(decision.AuditTrail.MatchedRules))
		for _, rule := range decision.AuditTrail.MatchedRules {
			fmt.Printf("    [%s] %s (bindings: %v)\n", rule.Tier, rule.RuleName, rule.Bindings)
		}
	} else {
		fmt.Println("  (read-only policy: no halt rules, so AuditTrail is empty)")
	}

	// Re-run the actual query to print the solutions the policy derived.
	results, qErr := client.Engine().Query(ctx, nil, `manages_star("alice", Y)`)
	if qErr != nil {
		log.Fatalf("Query failed: %v", qErr)
	}
	for _, r := range results {
		fmt.Printf("  -> alice transitively reports to: %s\n", r["Y"])
	}
}
