package main

import (
	"context"
	"fmt"
	"log"

	"github.com/duynguyendang/manglekit"
)

// KubernetesRequest represents the input for the guardrail.
// The mangle tags control how struct fields are flattened into Datalog facts.
type KubernetesRequest struct {
	Operation  string `mangle:"req_operation"`
	IsPeakHour string `mangle:"is_peak_hour"`
	Namespace  string `mangle:"pod_namespace"`
	Tier       string `mangle:"pod_tier"`
}

func main() {
	ctx := context.Background()

	client := manglekit.Must(manglekit.NewClient(
		ctx,
		manglekit.WithBlueprintPath("infrastructure_copilot/safety.dl"),
	))

	deletePod := func(ctx context.Context, req KubernetesRequest) (string, error) {
		return fmt.Sprintf("Executed %s on pod in %s", req.Operation, req.Namespace), nil
	}

	action := manglekit.Define(client, "k8s_guardrail", deletePod)

	logger := client.Logger()

	// Case A: Allowed Operation (Read in Production during peak)
	logger.Info("--- Case A: Read in Production (Allowed) ---")
	reqA := KubernetesRequest{
		Operation:  "READ",
		IsPeakHour: "true",
		Namespace:  "production",
		Tier:       "web",
	}
	if res, err := action.Run(ctx, reqA); err != nil {
		log.Fatalf("Unexpected block for Case A: %v", err)
	} else {
		logger.Info("Success", "result", res)
	}

	// Case B: Denied Operation (Delete Critical Pod)
	logger.Info("--- Case B: Delete Critical Pod (Denied) ---")
	reqB := KubernetesRequest{
		Operation:  "DELETE",
		IsPeakHour: "false",
		Namespace:  "default",
		Tier:       "critical",
	}
	if _, err := action.Run(ctx, reqB); err == nil {
		log.Fatalf("Unexpected success for Case B (Should be blocked)")
	} else {
		logger.Warn("Blocked as expected", "error", err)
	}

	// Case C: Denied Operation (Write in Production during Peak Hour)
	logger.Info("--- Case C: Update in Production during Peak Hour (Denied) ---")
	reqC := KubernetesRequest{
		Operation:  "UPDATE",
		IsPeakHour: "true",
		Namespace:  "production",
		Tier:       "web",
	}
	if _, err := action.Run(ctx, reqC); err == nil {
		log.Fatalf("Unexpected success for Case C (Should be blocked)")
	} else {
		logger.Warn("Blocked as expected", "error", err)
	}
}
