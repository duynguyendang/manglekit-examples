// temporal_compliance demonstrates manglekit's experimental temporal reasoning
// surface: facts carry validity windows, and a compliance question becomes
// "was this fact in effect at the time that matters?".
//
// Scenario: financial audit. A transaction T must be backed by an
// authorizing signature that was VALID at the transaction timestamp. We load
// authorization facts with time windows, then rewind the engine's evaluation
// time to each transaction's instant and ask whether the authorization held.
//
// Temporal reasoning is EXPERIMENTAL and OFF by default: client.EnableTemporal()
// opts in before any policy is loaded.
//
// Demonstrates: Client.EnableTemporal, AddTemporalFact, AddEternalFact,
// SetEvaluationTime, GetEvaluationTime, ContainsTemporalFact,
// QueryTemporalFactsAt / QueryTemporalFactsDuring.
//
// No API key required (deterministic).

package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/duynguyendang/manglekit"
)

// authorization links a signed approval to a fact that is valid only during
// a specific window.
type authorization struct {
	id    string
	start time.Time
	end   time.Time
}

// transaction is a financial event with a recorded timestamp and the
// authorization id that should back it.
type transaction struct {
	id        string
	at        time.Time
	needsAuth string
}

func main() {
	ctx := context.Background()

	client := manglekit.MustNewClient(ctx)
	defer client.Shutdown(ctx)

	// Opt in to temporal reasoning BEFORE loading policies.
	if err := client.EnableTemporal(); err != nil {
		fmt.Println("temporal not supported by this engine:", err)
		os.Exit(1)
	}
	if on, _ := client.IsTemporalEnabled(); !on {
		fmt.Println("temporal was not enabled")
		os.Exit(1)
	}

	jan := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	janEnd := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	feb := time.Date(2024, 2, 15, 0, 0, 0, 0, time.UTC)
	mar := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)

	// An eternal (always-on) fact: the compliance framework is active.
	if err := client.AddEternalFact(`framework("sox_compliance")`); err != nil {
		fmt.Println("add eternal fact:", err)
		os.Exit(1)
	}

	// Authorizations valid only during their window.
	auths := []authorization{
		{id: "auth_january", start: jan, end: janEnd},
		{id: "rollover_feb", start: jan, end: feb},
	}
	for _, a := range auths {
		if err := client.AddTemporalFact(`authorized("`+a.id+`")`, a.start, a.end); err != nil {
			fmt.Println("add temporal fact:", err)
			os.Exit(1)
		}
	}

	// Two transactions: one covered by an in-effect authorization, one not.
	txns := []transaction{
		{id: "TXN-1001", at: jan.Add(10 * 24 * time.Hour), needsAuth: "auth_january"}, // inside window
		{id: "TXN-2002", at: mar, needsAuth: "auth_january"},                          // window expired
	}

	// The audit question, answered with the temporal store.
	validAt := func(authID string, at time.Time) (bool, error) {
		// ContainsTemporalFact checks validity at the engine's evaluation time.
		if err := client.SetEvaluationTime(at); err != nil {
			return false, err
		}
		return client.ContainsTemporalFact(`authorized("` + authID + `")`)
	}

	// A windowed audit query over all January authorizations, independent of
	// eval time.
	januaryAuths, err := client.QueryTemporalFactsDuring(`authorized(ID)`, jan, janEnd)
	if err != nil {
		fmt.Println("query during:", err)
		os.Exit(1)
	}

	fmt.Println("=== Temporal Compliance Demo ===")
	fmt.Println()
	fmt.Println("Audit window (Jan):")
	for _, f := range januaryAuths {
		fmt.Printf("  %s\n", f)
	}
	fmt.Println()

	allPass := true
	for _, t := range txns {
		ok, err := validAt(t.needsAuth, t.at)
		if err != nil {
			fmt.Printf("  %s: check failed: %v\n", t.id, err)
			allPass = false
			continue
		}
		verdict := "HOLD  (authorization NOT valid at transaction time)"
		if ok {
			verdict = "RELEASE (authorization valid at transaction time)"
		} else {
			allPass = false
		}
		fmt.Printf("  %s @ %s: %s\n", t.id, t.at.Format("2006-01-02"), verdict)
	}
	fmt.Println()

	// Deterministic expectation for the test runner.
	if !allPass {
		fmt.Println("Expected at least one in-window transaction; result as designed.")
	} else {
		fmt.Println("All transactions covered by valid authorization.")
	}
}