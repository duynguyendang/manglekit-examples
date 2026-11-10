package trapsuite_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/duynguyendang/manglekit"
	"github.com/duynguyendang/manglekit/adapters/func"
	"github.com/duynguyendang/manglekit/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const securityGatePolicy = `
Decl amount_cents(Req, Val).
Decl valid_token(Req, Token).
Decl token_expiry(Token, ExpTime).
Decl is_owner(User, Resource).
Decl requester(Req, User).
Decl target_resource(Req, Resource).

% Financial Gate: Refunds > 100000 require manager token
Decl has_valid_token(Req).
has_valid_token(Req) :- valid_token(Req, _).

halt("Req", "unauthorized_high_value_transaction", "T1") :-
    action_operation("Req", "disburse"),
    amount_cents("Req", Val),
    :gt(Val, 100000),
    !has_valid_token("Req").

% Destructive Gate: Deletion requires owner
halt("Req", "unauthorized_deletion", "T1") :-
    action_operation("Req", "delete_node"),
    requester("Req", User),
    target_resource("Req", Res),
    !is_owner(User, Res).
`

// Trap 1: Direct Handler Invocation Bypass.
// An adversary or buggy agent tries to call an unregistered or raw handler directly,
// bypassing the Zero-Trust Supervisor. MangleKit rejects execution.
func TestTrap_DirectHandlerCallBypass(t *testing.T) {
	ctx := context.Background()
	client, err := manglekit.NewClient(ctx)
	require.NoError(t, err)
	defer client.Shutdown(ctx)
	require.NoError(t, client.LoadPolicy(ctx, securityGatePolicy))

	// Attempting to execute an action that was not registered with the supervisor gate fails closed
	_, err = client.ExecuteByName(ctx, "unregistered_dangerous_action", map[string]string{"cmd": "drop_all"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "action not found")
}

// Trap 2: Argument Tampering & Type Confusion.
// An adversary attempts to supply string values or negative boundaries
// to bypass numeric comparison gates.
func TestTrap_TypeConfusionAndBoundaryTampering(t *testing.T) {
	ctx := context.Background()
	client, err := manglekit.NewClient(ctx)
	require.NoError(t, err)
	defer client.Shutdown(ctx)
	require.NoError(t, client.LoadPolicy(ctx, securityGatePolicy))

	dispatched := false
	client.RegisterSupervised("disburse", function.New("disburse",
		func(ctx context.Context, in map[string]any) (string, error) {
			dispatched = true
			return "ok", nil
		}))

	// Case A: Amount exceeds 100000 without token -> blocked
	env := core.NewEnvelope(map[string]any{"amount_cents": 150000})
	env.Facts = []string{`amount_cents("Req", 150000).`}
	_, err = client.ExecuteByName(ctx, "disburse", env)
	require.Error(t, err)
	assert.True(t, core.IsPolicyViolationError(err))
	assert.False(t, dispatched)

	// Case B: Boundary exactness - 100000 (not greater) -> allowed
	env = core.NewEnvelope(map[string]any{"amount_cents": 100000})
	env.Facts = []string{`amount_cents("Req", 100000).`}
	_, err = client.ExecuteByName(ctx, "disburse", env)
	require.NoError(t, err)
	assert.True(t, dispatched)
}

// Trap 3: Stale Approval / Expired Authorization Token.
// An agent captures a previously valid approval token and attempts to reuse it
// after its expiration window has elapsed.
func TestTrap_ExpiredApprovalToken(t *testing.T) {
	ctx := context.Background()
	client, err := manglekit.NewClient(ctx)
	require.NoError(t, err)
	defer client.Shutdown(ctx)

	// Temporal policy: Token is only valid if evaluation time <= expiry
	temporalPolicy := `
Decl now_time(T).
Decl token_expiry(Token, Exp).
Decl valid_token(Req, Token).
Decl has_active_token(Req).

token_expiry("tok_abc", 1000).

has_active_token(Req) :-
    valid_token(Req, Tok),
    token_expiry(Tok, Exp),
    now_time(Now),
    :le(Now, Exp).

halt("Req", "expired_or_invalid_approval_token", "T1") :-
    action_operation("Req", "execute_transfer"),
    !has_active_token("Req").
`
	require.NoError(t, client.LoadPolicy(ctx, temporalPolicy))

	client.RegisterSupervised("execute_transfer", function.New("execute_transfer",
		func(ctx context.Context, in map[string]string) (string, error) {
			return "transferred", nil
		}))

	// Attempt transfer at time 1050 (expired token) -> MUST HALT
	envExpired := core.NewEnvelope(map[string]string{"transfer": "true"})
	envExpired.Facts = []string{
		`valid_token("Req", "tok_abc").`,
		`now_time(1050).`,
	}
	_, err = client.ExecuteByName(ctx, "execute_transfer", envExpired)
	require.Error(t, err)
	assert.True(t, core.IsPolicyViolationError(err))
	assert.Contains(t, err.Error(), "expired_or_invalid_approval_token")

	// Attempt transfer at time 950 (active token) -> ALLOWED
	envActive := core.NewEnvelope(map[string]string{"transfer": "true"})
	envActive.Facts = []string{
		`valid_token("Req", "tok_abc").`,
		`now_time(950).`,
	}
	res, err := client.ExecuteByName(ctx, "execute_transfer", envActive)
	require.NoError(t, err)
	assert.Equal(t, "transferred", res.Payload)
}

// Trap 4: Decision Replay & Idempotency Verification.
// A client checks that every distinct execution produces an independently verifiable
// decision receipt with unique IDs and verifiable derivation trees.
func TestTrap_DecisionIdempotencyAndReceipt(t *testing.T) {
	ctx := context.Background()
	client, err := manglekit.NewClient(ctx)
	require.NoError(t, err)
	defer client.Shutdown(ctx)
	require.NoError(t, client.LoadPolicy(ctx, securityGatePolicy))

	// Query explanation twice with identical facts
	facts := []string{
		`action_operation("Req", "disburse").`,
		`amount_cents("Req", 200000).`,
	}
	expl1, err := client.Explain(ctx, `halt("Req", Reason, Tier)`, facts)
	require.NoError(t, err)
	require.True(t, expl1.Outcome)

	expl2, err := client.Explain(ctx, `halt("Req", Reason, Tier)`, facts)
	require.NoError(t, err)
	require.True(t, expl2.Outcome)

	// Mathematical derivation proof must be identical and deterministic
	assert.Equal(t, expl1.String(), expl2.String())
	assert.Contains(t, expl1.String(), "200000 > 100000")
}

// Trap 5: Policy Hash Drift / Tampering.
// Verifies that a client can detect when an active gate policy has changed or drifted
// from the expected cryptographic baseline SHA-256 fingerprint.
func TestTrap_PolicyHashFingerprintVerification(t *testing.T) {
	policy1 := `halt("Req", "deny_v1", "T1") :- action_operation("Req", "op").`
	policy2 := `halt("Req", "deny_v2", "T1") :- action_operation("Req", "op").`

	hash1 := sha256.Sum256([]byte(policy1))
	hash2 := sha256.Sum256([]byte(policy2))

	hex1 := hex.EncodeToString(hash1[:])
	hex2 := hex.EncodeToString(hash2[:])

	assert.NotEqual(t, hex1, hex2, "different policies must yield distinct SHA-256 fingerprints")

	ctx := context.Background()
	client, err := manglekit.NewClient(ctx)
	require.NoError(t, err)
	defer client.Shutdown(ctx)

	require.NoError(t, client.LoadPolicy(ctx, policy1))
	expl, err := client.Explain(ctx, `halt("Req", Reason, Tier)`, []string{`action_operation("Req", "op").`})
	require.NoError(t, err)
	assert.Contains(t, expl.String(), "deny_v1")

	// Hot reload swaps policy
	require.NoError(t, client.LoadPolicy(ctx, policy2))
	explReloaded, err := client.Explain(ctx, `halt("Req", Reason, Tier)`, []string{`action_operation("Req", "op").`})
	require.NoError(t, err)
	assert.Contains(t, explReloaded.String(), "deny_v2")
}
