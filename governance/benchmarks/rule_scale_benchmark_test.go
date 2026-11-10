package benchmarks_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/duynguyendang/manglekit"
	"github.com/duynguyendang/manglekit/adapters/func"
	"github.com/duynguyendang/manglekit/core"
)

func generateRules(n int) string {
	var sb strings.Builder
	sb.WriteString("Decl tag(Req, Val).\n")
	sb.WriteString("Decl score_pm(Req, Val).\n")
	sb.WriteString("Decl policy_anchor(Marker).\n")
	sb.WriteString("policy_anchor(\"ok\").\n\n")

	for i := 1; i <= n; i++ {
		threshold := 500 + i*4
		fmt.Fprintf(&sb, "halt(\"Req\", \"deny_rule_%d\", \"T1\") :-\n", i)
		fmt.Fprintf(&sb, "    action_operation(\"Req\", \"execute_step\"),\n")
		fmt.Fprintf(&sb, "    tag(\"Req\", \"rule_%d\"),\n", i)
		fmt.Fprintf(&sb, "    score_pm(\"Req\", P),\n")
		fmt.Fprintf(&sb, "    :lt(P, %d).\n\n", threshold)
	}
	return sb.String()
}

func benchmarkRuleEvaluation(b *testing.B, ruleCount int) {
	ctx := context.Background()
	client, err := manglekit.NewClient(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer client.Shutdown(ctx)

	policy := generateRules(ruleCount)
	if err := client.LoadPolicy(ctx, policy); err != nil {
		b.Fatalf("failed to load %d rules: %v", ruleCount, err)
	}

	client.RegisterSupervised("execute_step", function.New("execute_step",
		func(ctx context.Context, in map[string]any) (string, error) {
			return "ok", nil
		}))

	env := core.NewEnvelope(map[string]any{"data": "test"})
	env.Facts = []string{
		fmt.Sprintf(`tag("Req", "rule_%d").`, ruleCount/2+1),
		`score_pm("Req", 950).`, // clears the threshold
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := client.ExecuteByName(ctx, "execute_step", env)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPolicyEvaluation_001_Rules(b *testing.B) {
	benchmarkRuleEvaluation(b, 1)
}

func BenchmarkPolicyEvaluation_010_Rules(b *testing.B) {
	benchmarkRuleEvaluation(b, 10)
}

func BenchmarkPolicyEvaluation_050_Rules(b *testing.B) {
	benchmarkRuleEvaluation(b, 50)
}

func BenchmarkPolicyEvaluation_100_Rules(b *testing.B) {
	benchmarkRuleEvaluation(b, 100)
}

func BenchmarkExplainDerivation_100_Rules(b *testing.B) {
	ctx := context.Background()
	client, err := manglekit.NewClient(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer client.Shutdown(ctx)

	policy := generateRules(100)
	if err := client.LoadPolicy(ctx, policy); err != nil {
		b.Fatal(err)
	}

	facts := []string{
		`action_operation("Req", "execute_step").`,
		`tag("Req", "rule_50").`,
		`score_pm("Req", 600).`, // 600 < 700 -> halts at rule_50
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		expl, err := client.Explain(ctx, `halt("Req", Reason, Tier)`, facts)
		if err != nil || !expl.Outcome {
			b.Fatalf("expected explain outcome true, got %v (err=%v)", expl.Outcome, err)
		}
	}
}
