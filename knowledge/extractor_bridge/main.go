// extractor_bridge demonstrates the neuro-symbolic bridge: LLM text→struct
// extraction feeding into the Datalog policy engine.
//
// The ExtractorAction takes free-text input, asks an LLM to extract
// structured data matching a target schema, and returns a typed struct.
// The struct's fields are then manually flattened to Datalog facts via
// mangle tags, enabling policy rules to reason over the extracted data.
//
// The mock LLM returns canned JSON. With GOOGLE_API_KEY set, a real
// Genkit model is used instead.
//
// No API key required (mock LLM).

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/duynguyendang/manglekit/adapters/extractor"
	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/sdk"
)

// Order represents the target schema for extraction.
type Order struct {
	Product  string  `mangle:"product" json:"product"`
	Quantity int     `mangle:"quantity" json:"quantity"`
	Price    float64 `mangle:"price" json:"price"`
}

// mockExtractLLM returns canned JSON matching the Order schema.
// Different inputs produce different order details.
type mockExtractLLM struct{}

func (a *mockExtractLLM) Execute(_ context.Context, env core.Envelope) (core.Envelope, error) {
	input, _ := env.Payload.(string)
	order := Order{Product: "widget", Quantity: 5, Price: 29.99}
	switch {
	case strings.Contains(strings.ToLower(input), "gadget"):
		order = Order{Product: "gadget", Quantity: 10, Price: 15.50}
	case strings.Contains(strings.ToLower(input), "purchase"):
		order = Order{Product: "widget", Quantity: 3, Price: 29.99}
	}
	data, _ := json.Marshal(order)
	return core.NewEnvelope(string(data)), nil
}

func (a *mockExtractLLM) Metadata() core.ActionMetadata {
	return core.ActionMetadata{Name: "mock_llm", Type: "mock"}
}

func main() {
	ctx := context.Background()

	fmt.Println("=== Extractor Bridge: Text → Struct → Datalog ===")
	fmt.Println()

	ext, err := extractor.New("order_extractor", &mockExtractLLM{}, Order{})
	if err != nil {
		log.Fatal(err)
	}

	inputs := []string{
		"I ordered 5 widgets at $29.99 each",
		"Buy 10 gadgets for $15.50 apiece",
		"Purchase 3 widgets at $29.99 each",
	}

	for _, text := range inputs {
		fmt.Printf("--- Input: %q ---\n", text)

		env := core.NewEnvelope(text)
		result, err := ext.Execute(ctx, env)
		if err != nil {
			fmt.Printf("  Extract error: %v\n", err)
			continue
		}

		if order, ok := result.Payload.(Order); ok {
			fmt.Printf("  Extracted: product=%s quantity=%d price=%.2f\n",
				order.Product, order.Quantity, order.Price)

			// The extracted struct can now be fed into the Datalog engine
			// via client.Engine().Assess() for policy evaluation.
			fmt.Printf("  → Datalog facts: product(\"Req\", %q), quantity(\"Req\", %d), price(\"Req\", %.2f)\n",
				order.Product, order.Quantity, order.Price)
			fmt.Printf("  → Price check: %.2f %s $50 threshold\n",
				order.Price, priceOp(order.Price))
		} else {
			fmt.Printf("  Unexpected payload type: %T\n", result.Payload)
		}
		fmt.Println()
	}

	// Show the full neuro-symbolic loop: extract → facts → policy
	fmt.Println("--- Full Neuro-Symbolic Loop ---")
	fmt.Println()

	client, err := sdk.NewClient(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Shutdown(ctx)

	// Declare the predicates the extractor will produce facts for.
	predicates := `Decl product(A, B).
Decl quantity(A, B).
Decl price(A, B).
Decl price_exceeds(A, B).`
	if err := client.Engine().LoadPolicy(ctx, predicates); err != nil {
		log.Fatal(err)
	}

	policy := `halt(Req, "Order exceeds budget") :- price_exceeds(Req, "true").`
	if err := client.Engine().LoadPolicy(ctx, policy); err != nil {
		log.Fatal(err)
	}

	orderEnv := core.NewEnvelope(Order{Product: "gadget", Quantity: 20, Price: 75.00})
	// Inject the extracted struct as Datalog facts (mangle struct tags).
	order, _ := orderEnv.Payload.(Order)
	orderEnv.Facts = append(orderEnv.Facts,
		fmt.Sprintf(`product("Req", %q).`, order.Product),
		fmt.Sprintf(`quantity("Req", %d).`, order.Quantity),
		fmt.Sprintf(`price("Req", %.2f).`, order.Price),
	)
	// Pre-compute numeric comparison in Go (Datalog can't cross-fact :lt/:gt).
	priceExceeds := "false"
	if order.Price > 50 {
		priceExceeds = "true"
	}
	orderEnv.Facts = append(orderEnv.Facts, fmt.Sprintf(`price_exceeds("Req", %q).`, priceExceeds))
	orderEnv.Facts = append(orderEnv.Facts, `action_operation("Req", "place_order").`)

	err = client.Engine().Assess(ctx, core.ActionMetadata{Name: "place_order"}, orderEnv)
	if err != nil {
		fmt.Printf("  Policy check: BLOCKED (%v)\n", err)
	} else {
		fmt.Println("  Policy check: ALLOWED")
	}

	fmt.Println()
	fmt.Println("Done.")
}

func priceOp(price float64) string {
	if price > 50 {
		return ">"
	}
	return "<="
}
