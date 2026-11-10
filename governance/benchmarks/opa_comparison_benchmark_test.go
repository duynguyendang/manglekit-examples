package benchmarks_test

import (
	"context"
	"testing"

	"github.com/duynguyendang/manglekit"
	"github.com/duynguyendang/manglekit/adapters/func"
	"github.com/duynguyendang/manglekit/core"
	"github.com/open-policy-agent/opa/rego"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const mangleRefundPolicy = `
Decl order(Req, Order).
Decl amount(Req, Amount).
Decl recipient(Req, Recipient).
Decl original_payer(Order, Payer).
Decl manager_approval(Order, Mgr).
Decl compliance_override(Order, Approver).
Decl revoked_account(User).

Decl has_manager_approval(Order).
has_manager_approval(Order) :- manager_approval(Order, _).

Decl has_compliance_override(Order).
has_compliance_override(Order) :- compliance_override(Order, _).

% Rule 1: Amount > 5000 requires manager approval (T1)
halt("Req", "high_refund_requires_manager", "T1") :-
    action_operation("Req", "refund"),
    order("Req", Order),
    amount("Req", Amount),
    :gt(Amount, 5000),
    !has_manager_approval(Order).

% Rule 2: Recipient mismatch requires compliance override (T1)
halt("Req", "recipient_mismatch", "T1") :-
    action_operation("Req", "refund"),
    order("Req", Order),
    recipient("Req", Recipient),
    !original_payer(Order, Recipient),
    !has_compliance_override(Order).

% Rule 3: Revoked account is blocked immediately (T0)
halt("Req", "revoked_account_blocked", "T0") :-
    action_operation("Req", "refund"),
    recipient("Req", Recipient),
    revoked_account(Recipient).
`

const regoRefundPolicy = `
package financial.guard

default allow = false

allow {
    count(deny) == 0
}

# Rule 1: High refund requires manager approval
deny[{"rule": "high_refund_requires_manager", "tier": "T1"}] {
    input.operation == "refund"
    input.amount > 5000
    not input.manager_approvals[input.order]
}

# Rule 2: Recipient mismatch requires compliance override
deny[{"rule": "recipient_mismatch", "tier": "T1"}] {
    input.operation == "refund"
    payer := input.original_payers[input.order]
    input.recipient != payer
    not input.compliance_overrides[input.order]
}

# Rule 3: Revoked account is blocked
deny[{"rule": "revoked_account_blocked", "tier": "T0"}] {
    input.operation == "refund"
    input.revoked_accounts[input.recipient]
}
`

type RefundRequest struct {
	Order     string `mangle:"order"`
	Amount    int64  `mangle:"amount"`
	Recipient string `mangle:"recipient"`
}

func setupMangleClient(t testing.TB) (*manglekit.Client, context.Context) {
	ctx := context.Background()
	client, err := manglekit.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if err := client.LoadPolicy(ctx, mangleRefundPolicy); err != nil {
		t.Fatalf("failed to load Mangle policy: %v", err)
	}

	client.RegisterSupervised("refund", function.New("refund",
		func(ctx context.Context, in RefundRequest) (string, error) {
			return "processed", nil
		}))

	return client, ctx
}

func setupOPAPreparedEval(t testing.TB) (rego.PreparedEvalQuery, context.Context) {
	ctx := context.Background()
	query, err := rego.New(
		rego.Query("data.financial.guard.deny"),
		rego.Module("refund.rego", regoRefundPolicy),
	).PrepareForEval(ctx)
	if err != nil {
		t.Fatalf("failed to prepare OPA query: %v", err)
	}
	return query, ctx
}

func Test_OPA_vs_MangleKit_Equivalence(t *testing.T) {
	mClient, mCtx := setupMangleClient(t)
	defer mClient.Shutdown(mCtx)

	opaQuery, opaCtx := setupOPAPreparedEval(t)

	tests := []struct {
		name                 string
		order                string
		amount               int64
		recipient            string
		extraMangleFacts     []string
		extraOPAPayers       map[string]any
		extraOPAApprovals    map[string]any
		extraOPAOverrides    map[string]any
		expectedAllow        bool
		expectedRule         string
	}{
		{
			name:          "Allowed: within limit, matching payer, approved",
			order:         "ord_101",
			amount:        7500,
			recipient:     "user_alice",
			expectedAllow: true,
		},
		{
			name:                 "Denied: amount > 5000 without manager approval",
			order:                "ord_999", // ord_999 has no manager approval
			amount:               8000,
			recipient:            "user_alice",
			extraMangleFacts:     []string{`original_payer("ord_999", "user_alice").`},
			extraOPAPayers:       map[string]any{"ord_999": "user_alice"},
			expectedAllow:        false,
			expectedRule:         "high_refund_requires_manager",
		},
		{
			name:          "Denied: recipient mismatch without compliance override",
			order:         "ord_101",
			amount:        3000,
			recipient:     "stranger_bob",
			expectedAllow: false,
			expectedRule:  "recipient_mismatch",
		},
		{
			name:          "Denied: revoked account",
			order:         "ord_101",
			amount:        1000,
			recipient:     "bad_actor",
			expectedAllow: false,
			expectedRule:  "revoked_account_blocked",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 1. Evaluate with MangleKit
			env := core.NewEnvelope(RefundRequest{
				Order:     tt.order,
				Amount:    tt.amount,
				Recipient: tt.recipient,
			})
			env.Facts = append([]string{
				`original_payer("ord_101", "user_alice").`,
				`manager_approval("ord_101", "mgr_carol").`,
				`revoked_account("bad_actor").`,
			}, tt.extraMangleFacts...)

			_, mErr := mClient.ExecuteByName(mCtx, "refund", env)
			if tt.expectedAllow {
				assert.NoError(t, mErr, "MangleKit should ALLOW")
			} else {
				require.Error(t, mErr, "MangleKit should DENY")
				var pve *core.PolicyViolationError
				if assert.ErrorAs(t, mErr, &pve) {
					assert.Equal(t, tt.expectedRule, pve.RuleID, "MangleKit RuleID mismatch")
				}
			}

			// 2. Evaluate with OPA
			payers := map[string]any{"ord_101": "user_alice"}
			for k, v := range tt.extraOPAPayers {
				payers[k] = v
			}
			approvals := map[string]any{"ord_101": "mgr_carol"}
			for k, v := range tt.extraOPAApprovals {
				approvals[k] = v
			}
			overrides := map[string]any{}
			for k, v := range tt.extraOPAOverrides {
				overrides[k] = v
			}

			input := map[string]any{
				"operation":            "refund",
				"order":                tt.order,
				"amount":               tt.amount,
				"recipient":            tt.recipient,
				"original_payers":      payers,
				"manager_approvals":    approvals,
				"compliance_overrides": overrides,
				"revoked_accounts":     map[string]any{"bad_actor": true},
			}
			rs, err := opaQuery.Eval(opaCtx, rego.EvalInput(input))
			require.NoError(t, err)
			require.Len(t, rs, 1)

			denials, ok := rs[0].Expressions[0].Value.([]any)
			require.True(t, ok)

			if tt.expectedAllow {
				assert.Empty(t, denials, "OPA should ALLOW")
			} else {
				assert.NotEmpty(t, denials, "OPA should DENY")
				foundRule := false
				for _, d := range denials {
					dm := d.(map[string]any)
					if dm["rule"] == tt.expectedRule {
						foundRule = true
						break
					}
				}
				assert.True(t, foundRule, "OPA denials should include %s (got %v)", tt.expectedRule, denials)
			}
		})
	}
}

// -----------------------------------------------------------------------
// Benchmarks: MangleKit vs OPA (In-Process Go Execution)
// -----------------------------------------------------------------------

func Benchmark_MangleKit_Evaluate_Allow(b *testing.B) {
	client, ctx := setupMangleClient(b)
	defer client.Shutdown(ctx)

	env := core.NewEnvelope(RefundRequest{
		Order:     "ord_101",
		Amount:    7500,
		Recipient: "user_alice",
	})
	env.Facts = []string{
		`original_payer("ord_101", "user_alice").`,
		`manager_approval("ord_101", "mgr_carol").`,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := client.ExecuteByName(ctx, "refund", env)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func Benchmark_OPA_Evaluate_Allow(b *testing.B) {
	query, ctx := setupOPAPreparedEval(b)
	input := map[string]any{
		"operation":            "refund",
		"order":                "ord_101",
		"amount":               7500,
		"recipient":            "user_alice",
		"original_payers":      map[string]any{"ord_101": "user_alice"},
		"manager_approvals":    map[string]any{"ord_101": "mgr_carol"},
		"compliance_overrides": map[string]any{},
		"revoked_accounts":     map[string]any{},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rs, err := query.Eval(ctx, rego.EvalInput(input))
		if err != nil || len(rs) == 0 {
			b.Fatal(err)
		}
	}
}

func Benchmark_MangleKit_Evaluate_Deny(b *testing.B) {
	client, ctx := setupMangleClient(b)
	defer client.Shutdown(ctx)

	env := core.NewEnvelope(RefundRequest{
		Order:     "ord_999", // No manager approval -> halts
		Amount:    7500,
		Recipient: "user_alice",
	})
	env.Facts = []string{
		`original_payer("ord_999", "user_alice").`,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := client.ExecuteByName(ctx, "refund", env)
		if err == nil {
			b.Fatal("expected halt")
		}
	}
}

func Benchmark_OPA_Evaluate_Deny(b *testing.B) {
	query, ctx := setupOPAPreparedEval(b)
	input := map[string]any{
		"operation":            "refund",
		"order":                "ord_999", // No manager approval -> denies
		"amount":               7500,
		"recipient":            "user_alice",
		"original_payers":      map[string]any{"ord_999": "user_alice"},
		"manager_approvals":    map[string]any{},
		"compliance_overrides": map[string]any{},
		"revoked_accounts":     map[string]any{},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rs, err := query.Eval(ctx, rego.EvalInput(input))
		if err != nil || len(rs) == 0 {
			b.Fatal(err)
		}
	}
}

func Benchmark_MangleKit_Explain_ProofTree(b *testing.B) {
	client, ctx := setupMangleClient(b)
	defer client.Shutdown(ctx)

	facts := []string{
		`action_operation("Req", "refund").`,
		`order("Req", "ord_999").`,
		`amount("Req", 7500).`,
		`recipient("Req", "user_alice").`,
		`original_payer("ord_999", "user_alice").`,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tree, err := client.Explain(ctx, `halt("Req", "high_refund_requires_manager", "T1").`, facts)
		if err != nil || tree == nil {
			b.Fatalf("failed to explain: %v", err)
		}
	}
}
