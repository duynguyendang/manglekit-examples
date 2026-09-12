package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/duynguyendang/manglekit/adapters/knowledge"
	"github.com/duynguyendang/manglekit/adapters/storage/session"
	"github.com/duynguyendang/manglekit/adapters/vector"
	"github.com/duynguyendang/manglekit/sdk/ports"
	"github.com/duynguyendang/manglekit/testutil"
)

func TestSessionStoreCRUD(t *testing.T) {
	ctx := context.Background()
	ss := session.NewSessionStore()
	ss.SetTTL(10 * time.Minute)

	// Create
	ss.Put(ctx, "test-session", "key1", session.NewSessionFact("subj1", "pred1", "obj1", "g1", 0))
	ss.Put(ctx, "test-session", "key2", session.NewSessionFact("subj2", "pred2", "obj2", "g2", 0))

	// Read
	f, err := ss.Get(ctx, "test-session", "key1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if f.Subject != "subj1" || f.Predicate != "pred1" || f.Object != "obj1" {
		t.Errorf("unexpected fact: %+v", f)
	}

	// List
	all, err := ss.GetAll(ctx, "test-session")
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("expected 2 facts, got %d", len(all))
	}

	// Delete
	ss.Delete(ctx, "test-session", "key1")
	_, err = ss.Get(ctx, "test-session", "key1")
	if err == nil {
		t.Error("expected error after delete")
	}

	// Clear
	ss.ClearSession(ctx, "test-session")
	if ss.Count(ctx) != 0 {
		t.Error("expected 0 sessions after clear")
	}
}

func TestSessionStoreExpiry(t *testing.T) {
	ctx := context.Background()
	ss := session.NewSessionStore()
	ss.SetTTL(1 * time.Nanosecond) // immediate expiry

	ss.Put(ctx, "exp-session", "key", session.NewSessionFact("s", "p", "o", "g", 0))
	time.Sleep(10 * time.Millisecond) // let it expire

	_, err := ss.Get(ctx, "exp-session", "key")
	if err == nil {
		t.Error("expected error for expired fact")
	}
}

func TestTransientFactsStore(t *testing.T) {
	ctx := context.Background()
	tf := session.NewTransientFactsStore()

	tf.Put(ctx, "ooda-test", "step1", &ports.TransientFact{Subject: "s1", Predicate: "p1", Object: "o1", Graph: "g1"})
	tf.Put(ctx, "ooda-test", "step2", &ports.TransientFact{Subject: "s2", Predicate: "p2", Object: "o2", Graph: "g2"})

	atoms, err := tf.ToAtoms(ctx, "ooda-test")
	if err != nil {
		t.Fatalf("ToAtoms: %v", err)
	}
	if len(atoms) != 2 {
		t.Errorf("expected 2 atoms, got %d", len(atoms))
	}

	quads, err := tf.ToQuads(ctx, "ooda-test")
	if err != nil {
		t.Fatalf("ToQuads: %v", err)
	}
	if len(quads) != 2 {
		t.Errorf("expected 2 quads, got %d", len(quads))
	}

	tf.ClearSession(ctx, "ooda-test")
}

func TestMockEmbedder(t *testing.T) {
	emb := testutil.NewLengthEmbedder()
	if emb.Dimension() != 2 {
		t.Errorf("expected dimension 2, got %d", emb.Dimension())
	}
	v, err := emb.Embed(context.Background(), "test")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(v) != 2 {
		t.Errorf("expected vector of length 2, got %d", len(v))
	}
}

func TestVectorStore(t *testing.T) {
	ctx := context.Background()
	store := vector.NewSimpleStore(testutil.NewLengthEmbedder())

	store.Upsert(ctx, "doc1", "alpha beta gamma")
	store.Upsert(ctx, "doc2", "delta epsilon")
	store.Upsert(ctx, "doc3", "alpha beta")

	results, err := store.Search(ctx, "alpha beta", 2)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected at least 1 result")
	}

	content, err := store.Get(ctx, "doc1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if content != "alpha beta gamma" {
		t.Errorf("expected 'alpha beta gamma', got %s", content)
	}
}

func TestNTriplesParsing(t *testing.T) {
	f, err := os.Open(filepath.Join(exampleDir(), "ontology.nt"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close()

	facts, err := knowledge.ParseNTriples(f)
	if err != nil {
		t.Fatalf("ParseNTriples: %v", err)
	}
	if len(facts) < 3 {
		t.Errorf("expected at least 3 facts, got %d", len(facts))
	}
}
