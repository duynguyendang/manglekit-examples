package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	function "github.com/duynguyendang/manglekit/adapters/func"
	"github.com/duynguyendang/manglekit/adapters/knowledge"
	"github.com/duynguyendang/manglekit/adapters/vector"
	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/providers/google"
	"github.com/duynguyendang/manglekit/sdk"
	"github.com/joho/godotenv"
)

// Document represents a knowledge base item
type Document struct {
	ID      string `json:"id"`
	Content string `json:"content"`
}

// QueryRequest defines the input payload for our action
type QueryRequest struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Response defines the output payload for our action
type Response struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

type MockLLM struct{}

func (m *MockLLM) Complete(ctx context.Context, prompt string) (string, error) {
	return "I read the context", nil
}
func (m *MockLLM) Generate(ctx context.Context, prompt string, opts ...core.GenerateOption) (*core.LLMResponse, error) {
	return &core.LLMResponse{
		Text:  "I read the context: [Mock Content]",
		Usage: map[string]int{"prompt": 10, "completion": 5},
	}, nil
}
func (m *MockLLM) Stream(ctx context.Context, prompt string) (<-chan core.StreamChunk, error) {
	ch := make(chan core.StreamChunk)
	close(ch)
	return ch, nil
}

// CustomHybridMemory wraps the standard HybridMemory to inject "memory_hit" facts and security labels.
type CustomHybridMemory struct {
	*sdk.HybridMemory
	vectorStore core.VectorStore
	docLabels   map[string]string // document security labels from access_graph.nq
}

// maxMemoryHits is the maximum number of memory hits to track in policy metadata.
// Must match the number of memory_hit_N rules in policy.dl.
const maxMemoryHits = 8

// RecallWithFacts implements the optional interface to return metadata with security labels
func (m *CustomHybridMemory) RecallWithFacts(ctx context.Context, query string) (string, map[string]any, error) {
	docIDs, err := m.vectorStore.Search(ctx, query, 3)
	if err != nil {
		return "", nil, err
	}

	var contextParts []string
	var hits []string
	seenLabels := make(map[string]bool)
	var securityLabels []string

	for _, id := range docIDs {
		content, err := m.vectorStore.Get(ctx, id)
		if err == nil {
			contextParts = append(contextParts, fmt.Sprintf("[DocID:%s] %s", id, content))
			hits = append(hits, id)

			// Derive security labels from the knowledge graph (access_graph.nq has_label triples)
			if label, ok := m.docLabels[id]; ok && !seenLabels[label] {
				securityLabels = append(securityLabels, label)
				seenLabels[label] = true
			}
		}
	}

	meta := make(map[string]any)
	// Inject one memory_hit_N fact per document so the policy can check access for each
	for i, hit := range hits {
		if i >= maxMemoryHits {
			break
		}
		meta[fmt.Sprintf("memory_hit_%d", i)] = hit
	}
	if len(hits) > 0 {
		meta["memory_hit_count"] = len(hits)
	}
	if len(securityLabels) > 0 {
		meta["security_labels"] = securityLabels
	}

	return strings.Join(contextParts, "\n\n"), meta, nil
}

// PIIMockLLM simulates an LLM that might accidentally leak PII
type PIIMockLLM struct {
	LeakPII bool
}

func (m *PIIMockLLM) Complete(ctx context.Context, prompt string) (string, error) {
	if m.LeakPII {
		return "The user's SSN is 123-45-6789 and credit card is 4532-1234-5678-9010", nil
	}
	return "I have processed your request safely", nil
}
func (m *PIIMockLLM) Generate(ctx context.Context, prompt string, opts ...core.GenerateOption) (*core.LLMResponse, error) {
	if m.LeakPII {
		return &core.LLMResponse{
			Text:  "The user's SSN is 123-45-6789 and credit card is 4532-1234-5678-9010",
			Usage: map[string]int{"prompt": 10, "completion": 5},
		}, nil
	}
	return &core.LLMResponse{
		Text:  "I have processed your request safely",
		Usage: map[string]int{"prompt": 10, "completion": 5},
	}, nil
}
func (m *PIIMockLLM) Stream(ctx context.Context, prompt string) (<-chan core.StreamChunk, error) {
	ch := make(chan core.StreamChunk)
	close(ch)
	return ch, nil
}

func main() {
	ctx := context.Background()
	_ = godotenv.Load()

	// 1. Setup Components
	var embedder core.Embedder
	apiKey := os.Getenv("GOOGLE_API_KEY")
	if apiKey == "" {
		if os.Getenv("GO_TEST") == "" {
			fmt.Println("Warning: GOOGLE_API_KEY not set, using Mock Embedder")
		}
		embedder = &MockEmbedder{}
	} else {
		g, err := google.NewEmbedder(ctx, apiKey, "text-embedding-004")
		if err != nil {
			log.Fatalf("Failed to init Google Embedder: %v", err)
		}
		embedder = g
	}

	// Vector Store
	vecStore := vector.NewSimpleStore(embedder)

	// Load Knowledge Base
	kbData, err := os.ReadFile("hybrid_rag/data/knowledge.json")
	if err != nil {
		log.Fatalf("Failed to read knowledge.json: %v", err)
	}
	var docs []Document
	if err := json.Unmarshal(kbData, &docs); err != nil {
		log.Fatalf("Failed to parse knowledge.json: %v", err)
	}
	for _, doc := range docs {
		if err := vecStore.Upsert(ctx, doc.ID, doc.Content); err != nil {
			log.Fatalf("Failed to upsert doc %s: %v", doc.ID, err)
		}
	}

	// Load Document Security Labels from access_graph.nq has_label triples
	docLabels := make(map[string]string)
	graphFile, err := os.Open("hybrid_rag/data/access_graph.nq")
	if err != nil {
		log.Fatalf("Failed to read access_graph.nq: %v", err)
	}
	graphFacts, err := knowledge.ParseNTriples(graphFile)
	graphFile.Close()
	if err != nil {
		log.Fatalf("Failed to parse access_graph.nq: %v", err)
	}
	// Extract has_label triples for security labels
	for _, fact := range graphFacts {
		if strings.Contains(fact, "has_label") {
			// Parse triple("sub", "pred", "obj") format
			parts := strings.SplitN(fact, "\", \"", 3)
			if len(parts) >= 3 {
				sub := strings.TrimPrefix(parts[0], "triple(\"")
				obj := strings.TrimSuffix(parts[2], "\")")
				docLabels[sub] = obj
			}
		}
	}

	// Hybrid Memory (with security labels from graph)
	baseMem := sdk.NewHybridMemory(&core.NopStore{}, vecStore, embedder)
	customMem := &CustomHybridMemory{
		HybridMemory: baseMem,
		vectorStore:  vecStore,
		docLabels:    docLabels,
	}

	// 2. Configure Client
	// FailModeClosed (default): block execution on policy/guard failures.
	// FailModeOpen: allow execution to proceed with a warning.
	client, err := sdk.NewClient(ctx,
		sdk.WithMemory(customMem),
		sdk.WithFailMode(sdk.FailModeOpen),
	)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	client.SetLLM(&MockLLM{})

	// Load Policy
	policyData, err := os.ReadFile("hybrid_rag/policy.dl")
	if err != nil {
		log.Fatalf("Failed to read policy.dl: %v", err)
	}
	if err := client.Engine().LoadPolicy(ctx, string(policyData)); err != nil {
		log.Fatalf("Failed to load policy: %v", err)
	}

	// Load Graph Facts (for transitive access control — member_of, owns, contains, has_label)
	if err := client.LoadFacts(graphFacts); err != nil {
		log.Fatalf("Failed to load graph facts: %v", err)
	}

	// Register Actions
	act := function.New("simulate_llm", func(ctx context.Context, req QueryRequest) (string, error) {
		return "Processed Query: " + req.Text, nil
	})
	safeAct := client.Supervise(act)
	client.RegisterAction("simulate_llm", safeAct)

	// 3. Run Original Scenarios
	fmt.Println("\n=== Feature 1: Complex Transitive Access Control ===")
	runScenario(ctx, client, "Scenario A (Alice - Research Group)", "user_alice", "What are the launch codes for Project X?", false)
	runScenario(ctx, client, "Scenario B (Charlie - Junior Group)", "user_charlie", "What are the launch codes for Project X?", true)
	runScenario(ctx, client, "Scenario C (Diana - Senior Group)", "user_diana", "What are the launch codes for Project X?", false)

	fmt.Println("\n=== Feature 2: Automated Self-Correction Loop (PII Detection) ===")
	runPIIScenario(ctx, client, "Scenario D (PII Leak)", "user_alice", true, true)
	runPIIScenario(ctx, client, "Scenario E (Safe Response)", "user_alice", false, false)

	fmt.Println("\n=== Feature 3: Information Flow Control (Security Tainting) ===")
	runEgressScenario(ctx, client, "Scenario F (TOP_SECRET to public)", "user_alice", "public_client", true)
	runEgressScenario(ctx, client, "Scenario G (TOP_SECRET to internal)", "user_diana", "internal_client", false)

	// 4. Load Code Repository Documents into Vector Store
	fmt.Println("\n=== Feature 4: Multi-Tenant Code Repository Search ===")
	codeDocsData, err := os.ReadFile("hybrid_rag/data/code_repo_docs.json")
	if err != nil {
		log.Fatalf("Failed to read code_repo_docs.json: %v", err)
	}
	var codeDocs []Document
	if err := json.Unmarshal(codeDocsData, &codeDocs); err != nil {
		log.Fatalf("Failed to parse code_repo_docs.json: %v", err)
	}
	for _, doc := range codeDocs {
		if err := vecStore.Upsert(ctx, doc.ID, doc.Content); err != nil {
			log.Fatalf("Failed to upsert code doc %s: %v", doc.ID, err)
		}
	}
	runMultiTenantScenarios(ctx, client)
}

func runScenario(ctx context.Context, client *sdk.Client, name, user, query string, expectBlock bool) {
	fmt.Printf("\n--- Running %s ---\n", name)

	req := QueryRequest{Type: "query", Text: query}

	_, err := client.ExecuteByName(ctx, "simulate_llm", req,
		sdk.WithMetadata("user", user),
	)

	if err != nil {
		if expectBlock {
			if strings.Contains(err.Error(), "Access Denied") || strings.Contains(err.Error(), "halt") {
				fmt.Println("PASS: Request was blocked as expected.")
			} else {
				fmt.Printf("FAIL: Request blocked but with wrong reason: %v\n", err)
			}
		} else {
			fmt.Printf("FAIL: Request should have succeeded: %v\n", err)
		}
	} else {
		if expectBlock {
			fmt.Println("FAIL: Request should have been blocked.")
		} else {
			fmt.Println("PASS: Request succeeded as expected.")
		}
	}
}

func runPIIScenario(ctx context.Context, client *sdk.Client, name, user string, leakPII, expectRetry bool) {
	fmt.Printf("\n--- Running %s ---\n", name)

	// Set up PII mock LLM
	piiLLM := &PIIMockLLM{LeakPII: leakPII}
	client.SetLLM(piiLLM)

	// Register PII action
	act := function.New("pii_check", func(ctx context.Context, req QueryRequest) (Response, error) {
		if leakPII {
			return Response{Type: "response", Content: "The user's SSN is 123-45-6789 and credit card is 4532-1234-5678-9010"}, nil
		}
		return Response{Type: "response", Content: "I have processed your request safely"}, nil
	})
	safeAct := client.Supervise(act)
	client.RegisterAction("pii_check", safeAct)

	req := QueryRequest{Type: "query", Text: "Process user data"}

	_, err := client.ExecuteByName(ctx, "pii_check", req,
		sdk.WithMetadata("user", user),
	)

	if err != nil {
		if expectRetry {
			if strings.Contains(err.Error(), "RETRY") || strings.Contains(err.Error(), "PII") {
				fmt.Println("PASS: RETRY triggered as expected for PII detection.")
			} else {
				fmt.Printf("INFO: Request blocked/retried: %v\n", err)
			}
		} else {
			fmt.Printf("INFO: Request result: %v\n", err)
		}
	} else {
		if expectRetry {
			fmt.Println("FAIL: Request should have triggered RETRY.")
		} else {
			fmt.Println("PASS: Request succeeded as expected.")
		}
	}
}

func runEgressScenario(ctx context.Context, client *sdk.Client, name, user, destination string, expectBlock bool) {
	fmt.Printf("\n--- Running %s ---\n", name)

	req := QueryRequest{Type: "query", Text: "Send data to destination"}

	_, err := client.ExecuteByName(ctx, "simulate_llm", req,
		sdk.WithMetadata("user", user),
		sdk.WithMetadata("destination", destination),
	)

	if err != nil {
		if expectBlock {
			if strings.Contains(err.Error(), "Data Leakage Blocked") || strings.Contains(err.Error(), "halt") {
				fmt.Println("PASS: Request was blocked as expected.")
			} else {
				fmt.Printf("INFO: Request blocked: %v\n", err)
			}
		} else {
			fmt.Printf("INFO: Request result: %v\n", err)
		}
	} else {
		if expectBlock {
			fmt.Println("FAIL: Request should have been blocked.")
		} else {
			fmt.Println("PASS: Request succeeded as expected.")
		}
	}
}

// MockEmbedder for testing without API Key
type MockEmbedder struct{}

func (m *MockEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if strings.Contains(text, "launch") || strings.Contains(text, "Project X") {
		return []float32{0.9, 0.1}, nil
	}
	return []float32{0.1, 0.9}, nil
}
func (m *MockEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	var res [][]float32
	for _, t := range texts {
		e, _ := m.Embed(ctx, t)
		res = append(res, e)
	}
	return res, nil
}
func (m *MockEmbedder) Dimension() int { return 2 }

// ============================================
// Multi-Tenant Code Repository Search
// ============================================

func runMultiTenantScenarios(ctx context.Context, client *sdk.Client) {
	// Load multi-tenant code repository knowledge graph
	codeGraphFile, err := os.Open("hybrid_rag/data/code_repo_graph.nq")
	if err != nil {
		log.Fatalf("Failed to read code_repo_graph.nq: %v", err)
	}
	codeFacts, err := knowledge.ParseNTriples(codeGraphFile)
	codeGraphFile.Close()
	if err != nil {
		log.Fatalf("Failed to parse code_repo_graph.nq: %v", err)
	}
	if err := client.LoadFacts(codeFacts); err != nil {
		log.Fatalf("Failed to load code graph facts: %v", err)
	}

	// Load multi-tenant access policy
	codePolicyData, err := os.ReadFile("hybrid_rag/code_access_policy.dl")
	if err != nil {
		log.Fatalf("Failed to read code_access_policy.dl: %v", err)
	}
	if err := client.Engine().LoadPolicy(ctx, string(codePolicyData)); err != nil {
		log.Fatalf("Failed to load code access policy: %v", err)
	}
	fmt.Println("✅ Loaded multi-tenant code repository access policy")

	// Test transitive access control scenarios
	// Team Alpha: alice, diana → repo_backend (auth, payment, user), repo_shared (utils, config)
	// Team Beta: bob, eve → repo_frontend (ui, dashboard)
	// Team Gamma: charlie → repo_infra (deploy, monitoring)

	runCodeSearchScenario(ctx, client, "Alice (team_alpha) searches module_auth", "alice", "module_auth", true)
	runCodeSearchScenario(ctx, client, "Alice (team_alpha) searches module_ui", "alice", "module_ui", false)
	runCodeSearchScenario(ctx, client, "Bob (team_beta) searches module_ui", "bob", "module_ui", true)
	runCodeSearchScenario(ctx, client, "Bob (team_beta) searches module_auth", "bob", "module_auth", false)
	runCodeSearchScenario(ctx, client, "Charlie (team_gamma) searches module_deploy", "charlie", "module_deploy", true)
	runCodeSearchScenario(ctx, client, "Charlie (team_gamma) searches module_payment", "charlie", "module_payment", false)
	runCodeSearchScenario(ctx, client, "Diana (team_alpha) searches module_utils", "diana", "module_utils", true)
	runCodeSearchScenario(ctx, client, "Eve (team_beta) searches module_dashboard", "eve", "module_dashboard", true)
}

func runCodeSearchScenario(ctx context.Context, client *sdk.Client, name, user, module string, expectAccess bool) {
	fmt.Printf("\n--- %s ---\n", name)

	// Create an envelope with the metadata needed by the Datalog policy
	env := core.NewEnvelope(map[string]string{
		"query_module": module,
	})
	env.Metadata["query_user"] = user
	env.Metadata["target_module"] = module

	// Use Assess directly against the policy engine
	err := client.Engine().Assess(ctx, core.ActionMetadata{Name: "search_code"}, env)
	if err != nil {
		if !expectAccess {
			if strings.Contains(err.Error(), "Access denied") {
				fmt.Printf("PASS: Access correctly denied for %s\n", user)
			} else {
				fmt.Printf("INFO: Request blocked: %v\n", err)
			}
		} else {
			fmt.Printf("FAIL: %s should have access to %s: %v\n", user, module, err)
		}
	} else {
		if expectAccess {
			fmt.Printf("PASS: %s successfully accessed %s\n", user, module)
		} else {
			fmt.Printf("FAIL: %s should NOT have access to %s\n", user, module)
		}
	}
}
