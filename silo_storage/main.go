// silo_storage demonstrates the Manglekit storage subsystem
// (`adapters/storage/session`, `adapters/knowledge`, `adapters/vector`):
//
//   - SessionStore: transient in-memory facts with TTL
//   - TransientFactsStore: OODA coordination facts via ports.TransientStore
//   - N-Triples parsing: load RDF triples into Datalog engine facts
//   - Vector store: in-memory cosine similarity with mock embedder
//   - KnowledgeBridge: MEB-backed KnowledgeStore (requires BadgerDB)
//     documented but not instantiated here (see ROADMAP Phase 14)
//
// No API key required (deterministic mock embedder).

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/duynguyendang/manglekit/adapters/knowledge"
	"github.com/duynguyendang/manglekit/adapters/storage/session"
	"github.com/duynguyendang/manglekit/adapters/vector"
	"github.com/duynguyendang/manglekit/sdk"
	"github.com/duynguyendang/manglekit/sdk/ports"
	"github.com/duynguyendang/manglekit/testutil"
)

func exampleDir() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Dir(filename)
}

// =========================================================================
// Main
// =========================================================================

func main() {
	ctx := context.Background()

	fmt.Println("Manglekit Storage Subsystem (The Silo)")
	fmt.Println("========================================")

	// -------------------------------------------------------------------
	// Demo 1: SessionStore — transient coordination facts with TTL
	// -------------------------------------------------------------------
	fmt.Println("\n--- Demo 1: SessionStore (Transient Facts with TTL) ---")

	ss := session.NewSessionStore()
	ss.SetTTL(30 * time.Minute)

	// Store some coordination facts
	ss.Put(ctx, "session-alpha", "current_step",
		session.NewSessionFact("workflow", "step", "researching", "t2", 0))
	ss.Put(ctx, "session-alpha", "agent_status",
		session.NewSessionFact("agent", "status", "busy", "default", 0))
	ss.Put(ctx, "session-alpha", "last_result",
		session.NewSessionFact("result", "type", "research_findings", "default", 0))

	fmt.Printf("  Session 'session-alpha' after 3 puts:\n")
	facts, _ := ss.GetAll(ctx, "session-alpha")
	for _, f := range facts {
		fmt.Printf("    %s(%s) = %s [graph=%s]\n", f.Predicate, f.Subject, f.Object, f.Graph)
	}

	// Read a specific fact
	fact, err := ss.Get(ctx, "session-alpha", "current_step")
	if err != nil {
		fmt.Printf("  Get error: %v\n", err)
	} else {
		fmt.Printf("  Fact 'current_step': %s(%s) = %s\n", fact.Predicate, fact.Subject, fact.Object)
	}

	// Count before cleanup
	fmt.Printf("  Total sessions: %d\n", ss.Count(ctx))
	// Clear and verify
	ss.ClearSession(ctx, "session-alpha")
	count := ss.Count(ctx)
	fmt.Printf("  After clear, sessions: %d\n", count)

	// -------------------------------------------------------------------
	// Demo 2: TransientFactsStore — OODA coordination via ports.TransientStore
	// -------------------------------------------------------------------
	fmt.Println("\n--- Demo 2: TransientFactsStore (OODA Coordination) ---")

	tf := session.NewTransientFactsStore()

	tf.Put(ctx, "ooda-001", "orient_result", &ports.TransientFact{
		Subject: "orient", Predicate: "result", Object: "context_loaded", Graph: "default",
	})
	tf.Put(ctx, "ooda-001", "decide_action", &ports.TransientFact{
		Subject: "decide", Predicate: "action", Object: "generate_report", Graph: "default",
	})
	tf.Put(ctx, "ooda-001", "act_result", &ports.TransientFact{
		Subject: "act", Predicate: "result", Object: "success", Graph: "default",
	})

	// Read back as atoms (for Brain evaluation)
	atoms, _ := tf.ToAtoms(ctx, "ooda-001")
	fmt.Printf("  OODA session 'ooda-001' as atoms:\n")
	for _, a := range atoms {
		fmt.Printf("    %s(%s) = %s\n", a.Predicate, a.Subject, a.Object)
	}

	// Read back as quads (for storage)
	quads, _ := tf.ToQuads(ctx, "ooda-001")
	fmt.Printf("  OODA session as quads: %d facts\n", len(quads))

	tf.ClearSession(ctx, "ooda-001")

	// -------------------------------------------------------------------
	// Demo 3: N-Triples knowledge graph loading into Datalog
	// -------------------------------------------------------------------
	fmt.Println("\n--- Demo 3: N-Triples Knowledge Graph Loading ---")

	f, err := os.Open(filepath.Join(exampleDir(), "ontology.nt"))
	if err != nil {
		fmt.Printf("  Warning: ontology.nt not found, skipping: %v\n", err)
		fmt.Println("  (Copy ontology.nt from knowledge_graph_reasoning/ to test)")
	} else {
		defer f.Close()
		facts, err := knowledge.ParseNTriples(f)
		if err != nil {
			fmt.Printf("  Parse error: %v\n", err)
		} else {
			fmt.Printf("  Parsed %d N-Triple facts\n", len(facts))
			for i, f := range facts {
				if i < 3 {
					fmt.Printf("    [%d] %s\n", i+1, f)
				}
			}
			if len(facts) > 3 {
				fmt.Printf("    ... and %d more\n", len(facts)-3)
			}
		}
	}

	// -------------------------------------------------------------------
	// Demo 4: Vector Store — similarity search with mock embedder
	// -------------------------------------------------------------------
	fmt.Println("\n--- Demo 4: Vector Store (Cosine Similarity with Mock Embedder) ---")

	store := vector.NewSimpleStore(testutil.NewLengthEmbedder())

	docs := map[string]string{
		"doc1": "microservices architecture design",
		"doc2": "kubernetes deployment guide",
		"doc3": "go programming language reference",
		"doc4": "datalog rule engine specification",
		"doc5": "api gateway configuration",
	}

	for id, content := range docs {
		if err := store.Upsert(ctx, id, content); err != nil {
			fmt.Printf("  Upsert error for %s: %v\n", id, err)
		}
	}
	fmt.Printf("  Indexed %d documents\n", len(docs))

	// Search
	query := "datalog specification"
	results, err := store.Search(ctx, query, 3)
	if err != nil {
		fmt.Printf("  Search error: %v\n", err)
	} else {
		fmt.Printf("  Search query: %q\n", query)
		fmt.Printf("  Top %d results:\n", len(results))
		for i, id := range results {
			content, _ := store.Get(ctx, id)
			fmt.Printf("    %d. %s: %s\n", i+1, id, content)
		}
	}

	// -------------------------------------------------------------------
	// Demo 5: End-to-end — load NTriples into Engine
	// -------------------------------------------------------------------
	fmt.Println("\n--- Demo 5: Load Knowledge Facts into Datalog Engine ---")

	client, err := sdk.NewClient(ctx)
	if err != nil {
		fmt.Printf("  NewClient error: %v\n", err)
		return
	}
	defer client.Shutdown(ctx)

	// Try to load from the knowledge_graph_reasoning example's ontology
	ontologyPath := filepath.Join(exampleDir(), "ontology.nt")
	if _, err := os.Stat(ontologyPath); os.IsNotExist(err) {
		// Fall back to a built-in small ontology
		facts := []string{
			`triple("alice", "reports_to", "bob")`,
			`triple("bob", "reports_to", "charlie")`,
			`triple("spec_alpha", "has_owner", "team_platform")`,
			`triple("alice", "member_of", "team_platform")`,
		}
		fmt.Println("  Using built-in small ontology (3 facts)")
		if err := client.LoadFacts(ctx, facts); err != nil {
			fmt.Printf("  LoadFacts error: %v\n", err)
		} else {
			// Query the facts via Datalog
			solutions, err := client.Engine().Query(ctx, nil, `triple("alice", "reports_to", Y)`)
			if err != nil {
				fmt.Printf("  Query error: %v\n", err)
			} else {
				fmt.Printf("  Query: triple(alice, reports_to, Y)\n")
				for _, s := range solutions {
					fmt.Printf("    alice reports_to %s\n", s["Y"])
				}
			}
		}
	} else {
		f, err := os.Open(ontologyPath)
		if err != nil {
			fmt.Printf("  Open error: %v\n", err)
		} else {
			defer f.Close()
			nfacts, pErr := knowledge.ParseNTriples(f)
			if pErr != nil {
				fmt.Printf("  Parse error: %v\n", pErr)
			} else {
				fmt.Printf("  Loaded %d facts from ontology.nt\n", len(nfacts))
				if err := client.LoadFacts(ctx, nfacts); err != nil {
					fmt.Printf("  LoadFacts error: %v\n", err)
				}
			}
		}
	}

	fmt.Println("\nDone.")
}
