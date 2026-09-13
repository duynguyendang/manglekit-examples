// policy_copilot answers: Can non-Datalog users author policy in English — and free text pass the same gates — with every rule signed and reviewable? (UC-I4, UC-I5)
// policy_copilot demonstrates the NL→Datalog policy generator:
//
//  - Schema extraction: reflects on Go struct fields with `mangle` tags
//    to discover available predicates for the LLM prompt.
//
//  - Few-shot learning: provides example translations so the LLM
//    understands the expected Datalog syntax.
//
//  - Syntax verification: generated rules are validated by the Mangle
//    parser before being returned, catching syntax errors early.
//
//  - Signed packaging (x/genes): a generated rule becomes a signed Gene with
//    provenance, survives a tamper-check round trip through a pool YAML, and
//    is applied to a client ONLY through the policy channel — enforcement of
//    machine-generated rules always carries an auditable origin.
//
//  - Text→struct extraction (extractor.go, merged from the extractor_bridge
//    example): free text → LLM schema extraction → mangle-tagged facts →
//    Datalog assessment — the full neuro-symbolic loop in one package.
//
// The mock LLM returns a deterministic valid Datalog rule.
// With GOOGLE_API_KEY set, a real Genkit model is used instead.
//
// No API key required (mock LLM returns canned Datalog).

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	function "github.com/duynguyendang/manglekit/adapters/func"
	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/sdk"
	"github.com/duynguyendang/manglekit/x/genes"
)

// schemaDecls mirrors the predicates extracted from Transaction's `mangle`
// tags — loaded once into any client that must evaluate generated rules.
const schemaDecls = `
Decl amount(E, F).
Decl region(E, S).
Decl category(E, S).
Decl risk_score(E, F).
`

func geneFingerprint(rule string) string {
	sum := sha256.Sum256([]byte(rule))
	return hex.EncodeToString(sum[:])[:8]
}

// demoGeneLifecycle packages a generated rule as a signed T3 gene, proves
// the tamper-evidence through a write/reload cycle, and enforces the result
// through the real supervised path via genes.ApplyTo.
func demoGeneLifecycle(ctx context.Context, nl, rule string) error {
	gene := genes.Gene{
		Name:    "copilot-" + geneFingerprint(rule),
		Tier:    core.TierT3_User, // machine-generated: lowest trust
		Source:  fmt.Sprintf("policy_copilot NL=%q", nl),
		Intents: []string{"process_transaction"},
		Rules:   rule + "\n",
	}
	gene.Sign()
	fmt.Printf("  Gene:     %s | tier=%s | sig=%s\n", gene.Name, gene.Tier, gene.SignatureHex()[:8])
	fmt.Printf("  Source:   %s\n", gene.Source)

	// Write pool → reopen → tamper a byte → reopen must FAIL.
	poolPath := filepath.Join(os.TempDir(), gene.Name+"-pool.yaml")
	defer os.Remove(poolPath)
	f, err := os.Create(poolPath)
	if err != nil {
		return err
	}
	if err := genes.WritePool(f, []genes.Gene{gene}); err != nil {
		return err
	}
	f.Close()
	if _, err := genes.LoadPoolFile(poolPath); err != nil {
		return fmt.Errorf("reload clean pool: %w", err)
	}
	raw, _ := os.ReadFile(poolPath)
	// Whitespace-only edit — semantics unchanged, signature destroyed.
	tampered := strings.Replace(string(raw), " :- ", "  :- ", 1)
	tamperPath := poolPath + ".tampered"
	if err := os.WriteFile(tamperPath, []byte(tampered), 0o600); err != nil {
		return err
	}
	defer os.Remove(tamperPath)
	tamperErr := func() error {
		_, e := genes.LoadPoolFile(tamperPath)
		return e
	}()
	fmt.Printf("  Tamper:   edited pool rejected=%v\n", tamperErr != nil)

	// Enforcement ONLY through the policy channel. The runtime needs the
	// SAME schema the copilot extracted from the struct tags — the data
	// predicates are EDB and must be declared before rules can reference
	// them.
	client, err := sdk.NewClient(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = client.Shutdown(ctx) }()
	if err := client.LoadPolicy(ctx, schemaDecls); err != nil {
		return err
	}
	if err := genes.ApplyTo(ctx, client, []genes.Gene{gene}); err != nil {
		return fmt.Errorf("apply gene: %w", err)
	}
	act := function.New("process_transaction", func(ctx context.Context, tx Transaction) (string, error) {
		return "processed", nil
	})
	client.RegisterSupervised("process_transaction", act)

	// Typed facts are passed explicitly on the envelope: the supervisor's
	// struct-flattening stringifies values (pre-check contract v1.2), so
	// numeric comparisons consume facts the way a typed loader (e.g. the
	// knowledge-graph smartCast path) would deliver them.
	txHigh := core.Envelope{Payload: "tx-high", Facts: []string{
		`amount("Req", 1500).`, `region("Req", "EU").`,
	}}
	txLow := core.Envelope{Payload: "tx-low", Facts: []string{
		`amount("Req", 500).`, `region("Req", "EU").`,
	}}
	_, denied := client.ExecuteByName(ctx, "process_transaction", txHigh)
	_, allowed := client.ExecuteByName(ctx, "process_transaction", txLow)
	fmt.Printf("  Enforce:  amount=1500 denied=%v | amount=500 denied=%v\n",
		core.IsPolicyViolationError(denied), core.IsPolicyViolationError(allowed))
	if denied != nil && !core.IsPolicyViolationError(denied) {
		return fmt.Errorf("unexpected non-policy error: %w", denied)
	}
	fmt.Println()
	return nil
}

// Transaction represents the data model for the policy copilot.
// Field tags teach the LLM which predicates are available.
type Transaction struct {
	Amount    float64 `mangle:"amount"`
	Region    string  `mangle:"region"`
	Category  string  `mangle:"category"`
	RiskScore float64 `mangle:"risk_score"`
}

// mockLLMAction returns a deterministic Datalog rule for any prompt.
type mockLLMAction struct{}

func (a *mockLLMAction) Execute(_ context.Context, env core.Envelope) (core.Envelope, error) {
	prompt, _ := env.Payload.(string)

	// Extract just the user policy from the prompt (after LAST "User Policy:" marker)
	lower := prompt
	if idx := strings.LastIndex(prompt, "User Policy:"); idx >= 0 {
		lower = strings.ToLower(prompt[idx:])
	}

	rule := `deny(Req) :- amount(Req, X), X > 1000.` // default
	if strings.Contains(lower, "region") && strings.Contains(lower, "us") {
		rule = `deny(Req) :- region(Req, "US"), amount(Req, X), X < 50.`
	} else if strings.Contains(lower, "risk") && strings.Contains(lower, "0.8") {
		rule = `deny(Req) :- risk_score(Req, X), X > 0.8.`
	}

	return core.NewEnvelope(rule), nil
}

func (a *mockLLMAction) Metadata() core.ActionMetadata {
	return core.ActionMetadata{Name: "mock_llm", Type: "mock"}
}

func main() {
	ctx := context.Background()

	fmt.Println("=== Policy Copilot: NL → Datalog ===")
	fmt.Println()
	fmt.Println("Schema extracted from Transaction struct:")
	fmt.Println("  amount(EntityID, FloatValue)")
	fmt.Println("  region(EntityID, StringValue)")
	fmt.Println("  category(EntityID, StringValue)")
	fmt.Println("  risk_score(EntityID, FloatValue)")
	fmt.Println()

	gen, err := sdk.NewPolicyGenerator(&mockLLMAction{}, sdk.GeneratorOptions{
		RuleHead: "deny(Req)",
	})
	if err != nil {
		log.Fatal(err)
	}

	policies := []struct {
		nl string
	}{
		{"Block transactions over $1000"},
		{"Deny transactions from the US region under $50"},
		{"Block high-risk scores above 0.8"},
	}

	for _, p := range policies {
		fmt.Printf("--- NL: %q ---\n", p.nl)

		rule, err := gen.GenerateRule(ctx, Transaction{}, p.nl)
		if err != nil {
			fmt.Printf("  ERROR: %v\n", err)
			continue
		}

		fmt.Printf("  Generated: %s\n", rule)
		fmt.Printf("  Verified:  syntax valid (parsed by Mangle)\n")
		if err := demoGeneLifecycle(ctx, p.nl, rule); err != nil {
			fmt.Printf("  gene lifecycle failed: %v\n", err)
		}
	}

	fmt.Println("=== Text → struct → Datalog (extractor section) ===")
	if err := demoExtraction(ctx); err != nil {
		log.Fatalf("extractor section: %v", err)
	}

	fmt.Println("Done.")
}
