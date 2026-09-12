// devops_policy_gate demonstrates infrastructure governance with Datalog
// security gates: numeric replica limits, business-hour restrictions on
// destructive operations, and approval/permission flags.
//
// Governed actions now run through client.Supervise + client.ExecuteByName.
// This is the headline Zero-Trust feature: Supervise wraps each action with
// the Zero-Trust Gatekeeper, whose PRE-CHECK halts the action before the
// inner operation ever runs when a halt rule fires. We deliberately do NOT
// call client.Engine().Assess(...) directly in the demo scenarios, because
// Assess bypasses the supervisor and therefore bypasses the gate — the
// "policy enforcement at each step" guarantee comes only from ExecuteByName.
//
// Governance semantics shown here (current code, verified):
//   - Both pre- and post-check are FAIL-CLOSED on verifier errors (ADR-001;
//     the old P0.1 fail-open note is obsolete — do not reintroduce it).
//   - halt tiers are REAL: T0/T1 block, explicitly-tagged T2/T3 are advisory
//     (P0.7), tier-less halts keep the fail-closed default.
//   - WithFailMode was removed in v0.6; fail-closed is the only mode.
//
// Scenarios 10-11 additionally demonstrate Explainable Governance:
// client.Explain prints the derivation tree, and structured deny errors
// carry Tier / MatchedRule / ActionName.
//
// The Mangle analyzer rejects cross-fact :lt/:lte built-ins, so the numeric
// comparisons themselves are performed in Go (see attachPrecomputedChecks).
// The caller pre-computes the boolean outcome (business_hours, scale_too_high,
// scale_too_low) and the policy just pattern-matches. This is the honest
// boundary: the Datalog layer is a gate, not a calculator.

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"runtime"
	"strconv"
	"sync/atomic"

	"github.com/duynguyendang/manglekit"
	"github.com/duynguyendang/manglekit/adapters/func"
	"github.com/duynguyendang/manglekit/core"
)

func exampleDir() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Dir(filename)
}

const (
	businessHoursStart = 9
	businessHoursEnd   = 18 // exclusive
	maxReplicas        = 10
	minReplicas        = 1
)

// injectNumericFact appends a numeric predicate fact to env.Facts so
// downstream Datalog queries can read structured numeric values (e.g. for
// pure Assess tests). The governed ExecuteByName path can consume these
// facts directly when the envelope is passed as the action input.
func injectNumericFact(env *core.Envelope, predicate string, n int) {
	env.Facts = append(env.Facts, fmt.Sprintf("%s(%d).", predicate, n))
}

// opCounter records whether a supervised no-op action actually executed, so
// the demo and tests can prove the inner operation was (or was not) reached.
type opCounter struct {
	executed int32
}

// run returns a no-op function that records its own execution and prints a
// trace line, mimicking the old MockAction so the demo shows whether the
// inner action fired.
func (c *opCounter) run(name string) func(context.Context, map[string]string) (string, error) {
	return func(_ context.Context, _ map[string]string) (string, error) {
		atomic.AddInt32(&c.executed, 1)
		fmt.Printf("-> Executing action: %s\n", name)
		return "ok", nil
	}
}

// attachPrecomputedChecks performs the numeric comparisons in Go and publishes
// the result as envelope Metadata (meta/2 facts) rather than raw Datalog
// facts. The supervised ExecuteByName path only forwards Metadata to the
// policy engine, so the boolean outcome must live there.
//
// ROADMAP.md §12 boundary ("Go computes, policy gates"): the numeric
// comparison is performed in Go and the boolean result is passed to the policy
// as a meta fact; the Datalog layer is the gate, not the calculator.
//
// These Go-computed values are trusted LOCAL data (not LLM-derived), so there
// is no escaping concern — they are boolean flags ("true"), not free text.
//
// Empty rawReplicas/rawHour are treated as "not applicable for this scenario"
// and produce no meta flag. This avoids spuriously emitting scale_too_low into
// a terraform-destroy envelope.
func attachPrecomputedChecks(env *core.Envelope, rawReplicas, rawHour string) {
	if env.Metadata == nil {
		env.Metadata = make(map[string]any)
	}
	if rawReplicas != "" {
		if n, err := strconv.Atoi(rawReplicas); err == nil {
			if n > maxReplicas {
				env.Metadata["scale_too_high"] = "true"
			}
			if n < minReplicas {
				env.Metadata["scale_too_low"] = "true"
			}
		}
	}
	if rawHour != "" {
		if h, err := strconv.Atoi(rawHour); err == nil {
			if h >= businessHoursStart && h < businessHoursEnd {
				env.Metadata["business_hours"] = "true"
			}
		}
	}
}

// executeGoverned forwards the given metadata (plus any Go-computed flags
// carried on extra.Metadata) to the supervised action via ExecuteByName.
// ExecuteByName forwards a caller-supplied envelope's explicit facts,
// labels, and metadata to the policy engine, so policy inputs can travel
// directly on the envelope — no per-key WithMetadata plumbing needed.
func executeGoverned(ctx context.Context, client *manglekit.Client, op string, meta map[string]string, extra *core.Envelope) (core.Envelope, error) {
	env := core.NewEnvelope(map[string]string{})
	for k, v := range meta {
		env.Metadata[k] = v
	}
	if extra != nil {
		for k, v := range extra.Metadata {
			if s, ok := v.(string); ok {
				env.Metadata[k] = s
			}
		}
		env.Facts = append(env.Facts, extra.Facts...)
		env.SecurityLabels = append(env.SecurityLabels, extra.SecurityLabels...)
	}
	return client.ExecuteByName(ctx, op, env)
}

// runGoverned executes a governed action and reports whether the inner
// (supervised) action actually ran, plus any error from ExecuteByName.
func runGoverned(ctx context.Context, client *manglekit.Client, op string, counter *int32, meta map[string]string, extra *core.Envelope) (ran bool, err error) {
	before := atomic.LoadInt32(counter)
	_, err = executeGoverned(ctx, client, op, meta, extra)
	after := atomic.LoadInt32(counter)
	return after > before, err
}

func main() {
	ctx := context.Background()

	fmt.Println("🚀 Secure CI/CD & DevOps Operator")
	fmt.Println("==================================")
	fmt.Println("Demonstrating infrastructure governance with Datalog security gates:")
	fmt.Println("1. Terraform/K8s operations intercepted by the Zero-Trust Gatekeeper")
	fmt.Println("2. Security rules enforce resource limits, time restrictions, and approvals")
	fmt.Println("3. Violations are blocked before reaching infrastructure")
	fmt.Println()

	// QuickClient constructs the client and loads the policy file in one
	// call, with typed errors for a missing file or an invalid policy.
	client, err := manglekit.QuickClient(ctx, filepath.Join(exampleDir(), "security_gate.dl"))
	if err != nil {
		log.Fatalf("Failed to initialize client with security gate policy: %v", err)
	}
	fmt.Println("🛡️  Loaded security_gate.dl policy (governance + pre-computed checks).")
	fmt.Println()

	// Register one supervised no-op action per operation name. Supervise wraps
	// each action with the Zero-Trust Gatekeeper (pre- AND post-check, both
	// fail-closed). Scenarios below are decided at the pre-check halt.
	var (
		scale   opCounter
		apply   opCounter
		deploy  opCounter
		destroy opCounter
	)

	client.RegisterSupervised("kubectl_scale", function.New("kubectl_scale", scale.run("kubectl_scale")))
	client.RegisterSupervised("terraform_apply", function.New("terraform_apply", apply.run("terraform_apply")))
	client.RegisterSupervised("kubectl_deploy", function.New("kubectl_deploy", deploy.run("kubectl_deploy")))
	client.RegisterSupervised("terraform_destroy", function.New("terraform_destroy", destroy.run("terraform_destroy")))
	fmt.Println("🔐 Registered supervised actions: kubectl_scale, terraform_apply, kubectl_deploy, terraform_destroy")
	fmt.Println()

	fmt.Println("🧪 Testing DevOps operations against security policies...")
	fmt.Println()

	// --- Scenario 1: Kubectl Scale to 20 (exceeds limit) ---
	fmt.Println("--- Scenario 1: Kubectl Scale to 20 Replicas (Should Block) ---")
	pre1 := &core.Envelope{}
	attachPrecomputedChecks(pre1, "20", "")
	meta1 := map[string]string{
		"deployment": "api-server",
		"namespace":  "production",
	}
	ran, err := runGoverned(ctx, client, "kubectl_scale", &scale.executed, meta1, pre1)
	if core.IsPolicyViolationError(err) && !ran {
		fmt.Printf("✅ Blocked: %v\n", err)
	} else {
		fmt.Println("❌ Unexpectedly allowed (should have blocked scale > 10)")
	}
	fmt.Println()

	// --- Scenario 2: Kubectl Scale to 5 (within limit) ---
	fmt.Println("--- Scenario 2: Kubectl Scale to 5 Replicas (Should Allow) ---")
	pre2 := &core.Envelope{}
	attachPrecomputedChecks(pre2, "5", "")
	meta2 := map[string]string{
		"deployment": "api-server",
		"namespace":  "production",
	}
	ran, err = runGoverned(ctx, client, "kubectl_scale", &scale.executed, meta2, pre2)
	if err == nil && ran {
		fmt.Println("✅ Allowed: Scale within limits.")
	} else {
		fmt.Printf("❌ Unexpectedly blocked: %v\n", err)
	}
	fmt.Println()

	// --- Scenario 3: Terraform Apply with Open Security Group ---
	fmt.Println("--- Scenario 3: Terraform Apply with Open Security Group (Should Block) ---")
	meta3 := map[string]string{
		"resource":                "aws_security_group",
		"name":                    "prod-api-sg",
		"has_open_security_group": "true",
	}
	ran, err = runGoverned(ctx, client, "terraform_apply", &apply.executed, meta3, nil)
	if core.IsPolicyViolationError(err) && !ran {
		fmt.Printf("✅ Blocked: %v\n", err)
	} else {
		fmt.Println("❌ Unexpectedly allowed (should have blocked open security group)")
	}
	fmt.Println()

	// --- Scenario 4: Terraform Apply with Proper Security Group ---
	fmt.Println("--- Scenario 4: Terraform Apply with Restricted Security Group (Should Allow) ---")
	meta4 := map[string]string{
		"resource":                "aws_security_group",
		"name":                    "prod-api-sg",
		"has_open_security_group": "false",
	}
	ran, err = runGoverned(ctx, client, "terraform_apply", &apply.executed, meta4, nil)
	if err == nil && ran {
		fmt.Println("✅ Allowed: Security group is properly restricted.")
	} else {
		fmt.Printf("❌ Unexpectedly blocked: %v\n", err)
	}
	fmt.Println()

	// --- Scenario 5: Production Deploy Without Approval ---
	fmt.Println("--- Scenario 5: Production Deploy Without Approval (Should Block) ---")
	meta5 := map[string]string{
		"image":        "api-server:v1.2.3",
		"target_env":   "production",
		"has_approval": "false",
	}
	ran, err = runGoverned(ctx, client, "kubectl_deploy", &deploy.executed, meta5, nil)
	if core.IsPolicyViolationError(err) && !ran {
		fmt.Printf("✅ Blocked: %v\n", err)
	} else {
		fmt.Println("❌ Unexpectedly allowed (should have blocked unapproved prod deploy)")
	}
	fmt.Println()

	// --- Scenario 6: Production Deploy With Approval ---
	fmt.Println("--- Scenario 6: Production Deploy With Approval (Should Allow) ---")
	meta6 := map[string]string{
		"image":             "api-server:v1.2.3",
		"target_env":        "production",
		"has_approval":      "true",
		"has_rollback_plan": "true",
	}
	ran, err = runGoverned(ctx, client, "kubectl_deploy", &deploy.executed, meta6, nil)
	if err == nil && ran {
		fmt.Println("✅ Allowed: Production deploy approved.")
	} else {
		fmt.Printf("❌ Unexpectedly blocked: %v\n", err)
	}
	fmt.Println()

	// --- Scenario 7: Terraform Destroy at 14:00 (Business Hours) ---
	fmt.Println("--- Scenario 7: Terraform Destroy at 14:00 UTC (Should Block) ---")
	pre7 := &core.Envelope{}
	attachPrecomputedChecks(pre7, "", "14")
	meta7 := map[string]string{
		"resource":     "aws_instance",
		"name":         "prod-db-01",
		"has_approval": "true",
	}
	ran, err = runGoverned(ctx, client, "terraform_destroy", &destroy.executed, meta7, pre7)
	if core.IsPolicyViolationError(err) && !ran {
		fmt.Printf("✅ Blocked: %v\n", err)
	} else {
		fmt.Println("❌ Unexpectedly allowed (should have blocked destroy during business hours)")
	}
	fmt.Println()

	// --- Scenario 8: Terraform Destroy at 22:00 (After Hours) ---
	fmt.Println("--- Scenario 8: Terraform Destroy at 22:00 UTC (Should Allow) ---")
	pre8 := &core.Envelope{}
	attachPrecomputedChecks(pre8, "", "22")
	meta8 := map[string]string{
		"resource":     "aws_instance",
		"name":         "staging-db-01",
		"has_approval": "true",
	}
	ran, err = runGoverned(ctx, client, "terraform_destroy", &destroy.executed, meta8, pre8)
	if err == nil && ran {
		fmt.Println("✅ Allowed: Terraform destroy approved outside business hours.")
	} else {
		fmt.Printf("❌ Unexpectedly blocked: %v\n", err)
	}
	fmt.Println()

	// --- Scenario 9: Public Database ---
	fmt.Println("--- Scenario 9: Terraform Apply with Public Database (Should Block) ---")
	meta9 := map[string]string{
		"resource":               "aws_db_instance",
		"name":                   "prod-postgres",
		"db_publicly_accessible": "true",
	}
	ran, err = runGoverned(ctx, client, "terraform_apply", &apply.executed, meta9, nil)
	if core.IsPolicyViolationError(err) && !ran {
		fmt.Printf("✅ Blocked: %v\n", err)
	} else {
		fmt.Println("❌ Unexpectedly allowed (should have blocked public database)")
	}
	fmt.Println()

	// --- Scenario 10: T1 governance rule BLOCKS + explainable deny ---
	fmt.Println("--- Scenario 10: Prod Deploy, approved but NO rollback plan (T1 rule → Block) ---")
	meta10 := map[string]string{
		"image":        "api-server:v2.0.0",
		"target_env":   "production",
		"has_approval": "true", // approval alone is no longer enough
	}
	ran, err = runGoverned(ctx, client, "kubectl_deploy", &deploy.executed, meta10, nil)
	if core.IsPolicyViolationError(err) && !ran {
		var pve *core.PolicyViolationError
		if errors.As(err, &pve) {
			fmt.Printf("✅ Blocked with structured provenance: tier=%s action=%s matched=%s\n",
				pve.Tier, pve.ActionName, pve.MatchedRule)
		} else {
			fmt.Printf("✅ Blocked: %v\n", err)
		}
	} else {
		fmt.Printf("❌ Expected T1 block, got ran=%v err=%v\n", ran, err)
	}
	// Explain the deny: backward-chain the query to a proof tree.
	expl, xerr := client.Explain(ctx, `halt("Req", Reason, Tier)`, []string{
		`action_operation("Req", "kubectl_deploy").`,
		`meta("target_env", "production").`,
		`meta("has_approval", "true").`,
	})
	if xerr != nil {
		fmt.Printf("explain failed: %v\n", xerr)
	} else {
		fmt.Printf("🔍 Why (Explain, outcome=%v):\n%s\n", expl.Outcome, expl.String())
	}
	fmt.Println()

	// --- Scenario 11: T2 playbook rule is ADVISORY — same policy, different tier ---
	fmt.Println("--- Scenario 11: Prod Deploy with rollback plan but no canary (T2 rule → Allow + visible advisory) ---")
	meta11 := map[string]string{
		"image":             "api-server:v2.0.0",
		"target_env":        "production",
		"has_approval":      "true",
		"has_rollback_plan": "true",
		"has_canary":        "false", // trips the T2 playbook rule
	}
	ran, err = runGoverned(ctx, client, "kubectl_deploy", &deploy.executed, meta11, nil)
	if err == nil && ran {
		fmt.Println("✅ Allowed: T2 is explicitly-tagged advisory — it never reaches the caller; Explain shows what the gate saw.")
		expl2, xerr2 := client.Explain(ctx, `halt("Req", Reason, Tier)`, []string{
			`action_operation("Req", "kubectl_deploy").`,
			`meta("target_env", "production").`,
			`meta("has_approval", "true").`,
			`meta("has_rollback_plan", "true").`,
			`meta("has_canary", "false").`,
		})
		if xerr2 == nil && expl2.Outcome {
			fmt.Printf("🔍 The policy DID have an opinion (Explain shows what the gate saw):\n%s\n", expl2.String())
		}
	} else {
		fmt.Printf("❌ T2 advisory must not block: ran=%v err=%v\n", ran, err)
	}
	fmt.Println()

	fmt.Println("✅ Secure CI/CD & DevOps Operator demonstration complete!")
	fmt.Println()
	fmt.Println("💡 Key Takeaway: Datalog policies act as a security gate between")
	fmt.Println("   the AI agent and critical infrastructure, preventing dangerous")
	fmt.Println("   operations before they reach Terraform/Kubernetes.")
}
