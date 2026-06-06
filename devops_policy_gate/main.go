package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/sdk"
)

func exampleDir() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Dir(filename)
}

func main() {
	ctx := context.Background()

	fmt.Println("🚀 Secure CI/CD & DevOps Operator")
	fmt.Println("==================================")
	fmt.Println("Demonstrating infrastructure governance with Datalog security gates:")
	fmt.Println("1. Terraform/K8s operations intercepted by policy engine")
	fmt.Println("2. Security rules enforce resource limits, time restrictions, and approvals")
	fmt.Println("3. Violations are blocked before reaching infrastructure")
	fmt.Println()

	// 1. Initialize Manglekit Client
	client, err := sdk.NewClient(ctx)
	if err != nil {
		log.Fatalf("Failed to initialize client: %v", err)
	}

	// 2. Load Security Gate Policy
	policyBytes, err := os.ReadFile(filepath.Join(exampleDir(), "security_gate.dl"))
	if err != nil {
		log.Fatalf("Failed to read security_gate.dl: %v", err)
	}

	if err := client.Engine().LoadPolicy(ctx, string(policyBytes)); err != nil {
		log.Fatalf("Failed to load security gate policy: %v", err)
	}
	fmt.Println("🛡️  Loaded security_gate.dl policy with 9 governance rules.")
	fmt.Println()

	// 3. Test Scenarios
	fmt.Println("🧪 Testing DevOps operations against security policies...")
	fmt.Println()

	// --- Scenario 1: Kubectl Scale (Exceeds Limit) ---
	fmt.Println("--- Scenario 1: Kubectl Scale to 20 Replicas (Should Block) ---")
	scaleEnv := core.NewEnvelope(map[string]string{
		"deployment": "api-server",
		"namespace":  "production",
	})
	scaleEnv.Metadata["target_replicas"] = "20"
	scaleEnv.Metadata["scale_too_high"] = "true"
	err = client.Engine().Assess(ctx, core.ActionMetadata{Name: "kubectl_scale"}, scaleEnv)
	if core.IsAlignmentError(err) {
		fmt.Printf("✅ Blocked: %v\n", err)
	} else {
		fmt.Println("❌ Unexpectedly allowed (should have blocked scale > 10)")
	}
	fmt.Println()

	// --- Scenario 2: Kubectl Scale (Within Limit) ---
	fmt.Println("--- Scenario 2: Kubectl Scale to 5 Replicas (Should Allow) ---")
	scaleOkEnv := core.NewEnvelope(map[string]string{
		"deployment": "api-server",
		"namespace":  "production",
	})
	scaleOkEnv.Metadata["target_replicas"] = "5"
	err = client.Engine().Assess(ctx, core.ActionMetadata{Name: "kubectl_scale"}, scaleOkEnv)
	if core.IsAlignmentError(err) {
		fmt.Printf("❌ Unexpectedly blocked: %v\n", err)
	} else {
		fmt.Println("✅ Allowed: Scale within limits.")
	}
	fmt.Println()

	// --- Scenario 3: Terraform Apply with Open Security Group ---
	fmt.Println("--- Scenario 3: Terraform Apply with Open Security Group (Should Block) ---")
	tfApplyEnv := core.NewEnvelope(map[string]string{
		"resource": "aws_security_group",
		"name":     "prod-api-sg",
	})
	tfApplyEnv.Metadata["has_open_security_group"] = "true"
	err = client.Engine().Assess(ctx, core.ActionMetadata{Name: "terraform_apply"}, tfApplyEnv)
	if core.IsAlignmentError(err) {
		fmt.Printf("✅ Blocked: %v\n", err)
	} else {
		fmt.Println("❌ Unexpectedly allowed (should have blocked open security group)")
	}
	fmt.Println()

	// --- Scenario 4: Terraform Apply with Proper Security Group ---
	fmt.Println("--- Scenario 4: Terraform Apply with Restricted Security Group (Should Allow) ---")
	tfApplyOkEnv := core.NewEnvelope(map[string]string{
		"resource": "aws_security_group",
		"name":     "prod-api-sg",
	})
	tfApplyOkEnv.Metadata["has_open_security_group"] = "false"
	err = client.Engine().Assess(ctx, core.ActionMetadata{Name: "terraform_apply"}, tfApplyOkEnv)
	if core.IsAlignmentError(err) {
		fmt.Printf("❌ Unexpectedly blocked: %v\n", err)
	} else {
		fmt.Println("✅ Allowed: Security group is properly restricted.")
	}
	fmt.Println()

	// --- Scenario 5: Production Deploy Without Approval ---
	fmt.Println("--- Scenario 5: Production Deploy Without Approval (Should Block) ---")
	deployEnv := core.NewEnvelope(map[string]string{
		"image": "api-server:v1.2.3",
	})
	deployEnv.Metadata["target_env"] = "production"
	deployEnv.Metadata["has_approval"] = "false"
	err = client.Engine().Assess(ctx, core.ActionMetadata{Name: "kubectl_deploy"}, deployEnv)
	if core.IsAlignmentError(err) {
		fmt.Printf("✅ Blocked: %v\n", err)
	} else {
		fmt.Println("❌ Unexpectedly allowed (should have blocked unapproved prod deploy)")
	}
	fmt.Println()

	// --- Scenario 6: Production Deploy With Approval ---
	fmt.Println("--- Scenario 6: Production Deploy With Approval (Should Allow) ---")
	deployApprovedEnv := core.NewEnvelope(map[string]string{
		"image": "api-server:v1.2.3",
	})
	deployApprovedEnv.Metadata["target_env"] = "production"
	deployApprovedEnv.Metadata["has_approval"] = "true"
	err = client.Engine().Assess(ctx, core.ActionMetadata{Name: "kubectl_deploy"}, deployApprovedEnv)
	if core.IsAlignmentError(err) {
		fmt.Printf("❌ Unexpectedly blocked: %v\n", err)
	} else {
		fmt.Println("✅ Allowed: Production deploy approved.")
	}
	fmt.Println()

	// --- Scenario 7: Terraform Destroy at 14:00 (Business Hours) ---
	fmt.Println("--- Scenario 7: Terraform Destroy at 14:00 UTC (Should Block) ---")
	destroyEnv := core.NewEnvelope(map[string]string{
		"resource": "aws_instance",
		"name":     "prod-db-01",
	})
	destroyEnv.Metadata["current_hour"] = "14"
	destroyEnv.Metadata["has_approval"] = "true"
	err = client.Engine().Assess(ctx, core.ActionMetadata{Name: "terraform_destroy"}, destroyEnv)
	if core.IsAlignmentError(err) {
		fmt.Printf("✅ Blocked: %v\n", err)
	} else {
		fmt.Println("❌ Unexpectedly allowed (should have blocked destroy during business hours)")
	}
	fmt.Println()

	// --- Scenario 8: Terraform Destroy at 22:00 (After Hours) ---
	fmt.Println("--- Scenario 8: Terraform Destroy at 22:00 UTC (Should Allow) ---")
	destroyNightEnv := core.NewEnvelope(map[string]string{
		"resource": "aws_instance",
		"name":     "staging-db-01",
	})
	destroyNightEnv.Metadata["current_hour"] = "22"
	destroyNightEnv.Metadata["has_approval"] = "true"
	err = client.Engine().Assess(ctx, core.ActionMetadata{Name: "terraform_destroy"}, destroyNightEnv)
	if core.IsAlignmentError(err) {
		fmt.Printf("❌ Unexpectedly blocked: %v\n", err)
	} else {
		fmt.Println("✅ Allowed: Terraform destroy approved outside business hours.")
	}
	fmt.Println()

	// --- Scenario 9: Public Database ---
	fmt.Println("--- Scenario 9: Terraform Apply with Public Database (Should Block) ---")
	publicDbEnv := core.NewEnvelope(map[string]string{
		"resource": "aws_db_instance",
		"name":     "prod-postgres",
	})
	publicDbEnv.Metadata["db_publicly_accessible"] = "true"
	err = client.Engine().Assess(ctx, core.ActionMetadata{Name: "terraform_apply"}, publicDbEnv)
	if core.IsAlignmentError(err) {
		fmt.Printf("✅ Blocked: %v\n", err)
	} else {
		fmt.Println("❌ Unexpectedly allowed (should have blocked public database)")
	}
	fmt.Println()

	fmt.Println("✅ Secure CI/CD & DevOps Operator demonstration complete!")
	fmt.Println()
	fmt.Println("💡 Key Takeaway: Datalog policies act as a security gate between")
	fmt.Println("   the AI agent and critical infrastructure, preventing dangerous")
	fmt.Println("   operations before they reach Terraform/Kubernetes.")
}
