package main

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"runtime"

	"github.com/duynguyendang/manglekit"
	"github.com/duynguyendang/manglekit/adapters/func"
	"github.com/duynguyendang/manglekit/core"
)

func exampleDir() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Dir(filename)
}

// RefundRequest models a payment disbursement action.
type RefundRequest struct {
	AmountCents int    `mangle:"amount_cents"`
	Destination string `mangle:"destination_account"`
}

// DeleteRequest models a destructive infrastructure deletion.
type DeleteRequest struct {
	Resource string `mangle:"target_resource"`
	Env      string `mangle:"resource_env"`
	User     string `mangle:"requester"`
}

// MessageRequest models sending outbound communications.
type MessageRequest struct {
	Type        string `mangle:"message_type"`
	Recipient   string `mangle:"recipient_phone"`
	ContainsPII string `mangle:"contains_pii"`
}

func main() {
	ctx := context.Background()

	policyPath := filepath.Join(exampleDir(), "enterprise_guards.mg")
	policyData := manglekit.MustReadFile(policyPath)

	client, err := manglekit.NewClient(ctx)
	if err != nil {
		log.Fatalf("failed to init client: %v", err)
	}
	defer client.Shutdown(ctx)

	if err := client.LoadPolicy(ctx, string(policyData)); err != nil {
		log.Fatalf("failed to load policy: %v", err)
	}

	// Register supervised actions
	client.RegisterSupervised("process_refund", function.New("process_refund",
		func(ctx context.Context, in RefundRequest) (string, error) {
			return fmt.Sprintf("Refund processed: %d cents to %s", in.AmountCents, in.Destination), nil
		}))

	client.RegisterSupervised("delete_resource", function.New("delete_resource",
		func(ctx context.Context, in DeleteRequest) (string, error) {
			return fmt.Sprintf("Resource deleted: %s (%s)", in.Resource, in.Env), nil
		}))

	client.RegisterSupervised("send_message", function.New("send_message",
		func(ctx context.Context, in MessageRequest) (string, error) {
			return fmt.Sprintf("Message sent to %s (type: %s)", in.Recipient, in.Type), nil
		}))

	fmt.Println("=== MangleKit Enterprise Guards Showcase ===")

	// 1. Financial Refund Guard (Disallowed: Exceeds threshold without manager approval)
	fmt.Println("\n[1] Testing Financial Refund Guard ($1,500 without manager approval)...")
	_, err = client.ExecuteByName(ctx, "process_refund", RefundRequest{
		AmountCents: 150000,
		Destination: "acc_verified_123",
	})
	if err != nil {
		fmt.Printf("🛑 BLOCKED: %v\n", err)
		printExplanation(client, []string{
			`action_operation("Req", "process_refund").`,
			`amount_cents("Req", 150000).`,
			`destination_account("Req", "acc_verified_123").`,
		})
	}

	// 2. Destructive Resource Delete Guard (Disallowed: Production deletion without breakglass token)
	fmt.Println("\n[2] Testing Destructive Delete Guard (delete in production without breakglass)...")
	_, err = client.ExecuteByName(ctx, "delete_resource", DeleteRequest{
		Resource: "db-prod-primary",
		Env:      "production",
		User:     "alice",
	})
	if err != nil {
		fmt.Printf("🛑 BLOCKED: %v\n", err)
		printExplanation(client, []string{
			`action_operation("Req", "delete_resource").`,
			`target_resource("Req", "db-prod-primary").`,
			`resource_env("Req", "production").`,
			`requester("Req", "alice").`,
		})
	}

	// 3. Communication / OTP Guard (Disallowed: OTP sent to unverified phone number)
	fmt.Println("\n[3] Testing Communication Guard (OTP auth code sent to unverified phone)...")
	_, err = client.ExecuteByName(ctx, "send_message", MessageRequest{
		Type:      "otp_auth_code",
		Recipient: "+15559990000", // unverified
	})
	if err != nil {
		fmt.Printf("🛑 BLOCKED: %v\n", err)
		printExplanation(client, []string{
			`action_operation("Req", "send_message").`,
			`message_type("Req", "otp_auth_code").`,
			`recipient_phone("Req", "+15559990000").`,
		})
	}

	// 4. Allowed execution: Refund with manager approval
	fmt.Println("\n[4] Testing Allowed Case: Refund with Manager Approval fact...")
	allowedEnv := core.NewEnvelope(RefundRequest{
		AmountCents: 150000,
		Destination: "acc_verified_123",
	})
	allowedEnv.Facts = []string{`manager_approval("Req", "manager_bob").`}
	res, err := client.ExecuteByName(ctx, "process_refund", allowedEnv)
	if err != nil {
		log.Fatalf("unexpected error: %v", err)
	}
	fmt.Printf("✅ ALLOWED: %s\n", res.Payload)
}

func printExplanation(client *manglekit.Client, facts []string) {
	expl, err := client.Explain(context.Background(), `halt("Req", Reason, Tier)`, facts)
	if err != nil {
		fmt.Printf("   Explain error: %v\n", err)
		return
	}
	fmt.Printf("   --- Proof Tree ---\n%s\n", expl.String())
}
