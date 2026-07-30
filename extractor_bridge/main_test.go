package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/duynguyendang/manglekit/adapters/extractor"
	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/sdk"
)

func TestExtractorReturnsStruct(t *testing.T) {
	ctx := context.Background()
	ext, err := extractor.New("test_extractor", &mockExtractLLM{}, Order{})
	if err != nil {
		t.Fatal(err)
	}

	result, err := ext.Execute(ctx, newEnvelope("test input"))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	order, ok := result.Payload.(Order)
	if !ok {
		t.Fatalf("expected Order, got %T", result.Payload)
	}
	if order.Product != "widget" {
		t.Errorf("expected product=widget, got %s", order.Product)
	}
	if order.Quantity != 5 {
		t.Errorf("expected quantity=5, got %d", order.Quantity)
	}
	if order.Price != 29.99 {
		t.Errorf("expected price=29.99, got %.2f", order.Price)
	}
}

func TestExtractorMetadata(t *testing.T) {
	ext, err := extractor.New("order_ext", &mockExtractLLM{}, Order{})
	if err != nil {
		t.Fatal(err)
	}
	meta := ext.Metadata()
	if meta.Name != "order_ext" {
		t.Errorf("expected name=order_ext, got %s", meta.Name)
	}
	if meta.Type != "extractor" {
		t.Errorf("expected type=extractor, got %s", meta.Type)
	}
}

func TestNeuroSymbolicLoop(t *testing.T) {
	ctx := context.Background()

	client, err := sdk.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Shutdown(ctx)

	predicates := `Decl product(A, B).
Decl quantity(A, B).
Decl price(A, B).
Decl price_exceeds(A, B).`
	if err := client.Engine().LoadPolicy(ctx, predicates); err != nil {
		t.Fatal(err)
	}

	policy := `halt(Req, "Order exceeds budget") :- price_exceeds(Req, "true").`
	if err := client.Engine().LoadPolicy(ctx, policy); err != nil {
		t.Fatal(err)
	}

	// High-price order should be blocked
	orderEnv := core.NewEnvelope(Order{Product: "gadget", Quantity: 20, Price: 75.00})
	order := orderEnv.Payload.(Order)
	orderEnv.Facts = append(orderEnv.Facts,
		fmt.Sprintf(`product("Req", %q).`, order.Product),
		fmt.Sprintf(`quantity("Req", %d).`, order.Quantity),
		fmt.Sprintf(`price("Req", %.2f).`, order.Price),
	)
	orderEnv.Facts = append(orderEnv.Facts, `price_exceeds("Req", "true").`)
	orderEnv.Facts = append(orderEnv.Facts, `action_operation("Req", "place_order").`)

	err = client.Engine().Assess(ctx, core.ActionMetadata{Name: "place_order"}, orderEnv)
	if err == nil {
		t.Error("expected policy to block high-price order")
	}

	// Low-price order should be allowed
	lowEnv := core.NewEnvelope(Order{Product: "widget", Quantity: 1, Price: 10.00})
	lowOrder := lowEnv.Payload.(Order)
	lowEnv.Facts = append(lowEnv.Facts,
		fmt.Sprintf(`product("Req", %q).`, lowOrder.Product),
		fmt.Sprintf(`quantity("Req", %d).`, lowOrder.Quantity),
		fmt.Sprintf(`price("Req", %.2f).`, lowOrder.Price),
	)
	lowEnv.Facts = append(lowEnv.Facts, `price_exceeds("Req", "false").`)
	lowEnv.Facts = append(lowEnv.Facts, `action_operation("Req", "place_order").`)

	err = client.Engine().Assess(ctx, core.ActionMetadata{Name: "place_order"}, lowEnv)
	if err != nil {
		t.Errorf("expected low-price order to be allowed, got: %v", err)
	}
}

func newEnvelope(payload string) core.Envelope {
	return core.NewEnvelope(payload)
}
