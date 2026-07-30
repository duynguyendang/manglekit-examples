// multi_agent_research demonstrates the Multi-Agent Runtime subsystem
// (`manglekit/multiagent/`):
//   - AgentSystem, WorkflowExecutor, ParallelWorkflowExecutor
//   - HydratedWorkflowExecutor, DatalogWorkflowLoader, EvaluateCondition
//
// No API key required (deterministic node executor).

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"

	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/multiagent"
)

func exampleDir() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Dir(filename)
}

type researchNodeExecutor struct {
	counter atomic.Int64
	order   *[]string
}

func (e *researchNodeExecutor) Execute(_ context.Context, node *multiagent.WorkflowNode, input interface{}, agent *multiagent.Agent) (interface{}, error) {
	seq := e.counter.Add(1)
	if e.order != nil {
		*e.order = append(*e.order, fmt.Sprintf("seq=%d node=%q agent=%q", seq, node.ID, agent.ID))
	}
	return fmt.Sprintf("[seq=%d] node=%q agent=%q role=%q input=%v", seq, node.ID, agent.ID, node.Agent, input), nil
}

type inMemorySessionStore struct {
	instances map[string]*core.WorkflowInstance
}

func newInMemorySessionStore() *inMemorySessionStore {
	return &inMemorySessionStore{instances: make(map[string]*core.WorkflowInstance)}
}

func (s *inMemorySessionStore) Create(_ context.Context, inst *core.WorkflowInstance) error {
	s.instances[inst.SessionKey()] = inst
	return nil
}

func (s *inMemorySessionStore) Get(_ context.Context, key string) (*core.WorkflowInstance, error) {
	inst, ok := s.instances[key]
	if !ok {
		return nil, fmt.Errorf("session not found: %s", key)
	}
	return inst, nil
}

func (s *inMemorySessionStore) Update(_ context.Context, inst *core.WorkflowInstance) error {
	s.instances[inst.SessionKey()] = inst
	return nil
}

func (s *inMemorySessionStore) Delete(_ context.Context, key string) error {
	delete(s.instances, key)
	return nil
}

func (s *inMemorySessionStore) Exists(_ context.Context, key string) bool {
	_, ok := s.instances[key]
	return ok
}

func (s *inMemorySessionStore) List(_ context.Context, sessionID string) ([]*core.WorkflowInstance, error) {
	var out []*core.WorkflowInstance
	for _, inst := range s.instances {
		if inst.SessionID == sessionID {
			out = append(out, inst)
		}
	}
	return out, nil
}

func (s *inMemorySessionStore) ClearSession(_ context.Context, sessionID string) error {
	for k, inst := range s.instances {
		if inst.SessionID == sessionID {
			delete(s.instances, k)
		}
	}
	return nil
}

type fixedAgentFinder struct {
	roleToAgent map[string]string
}

func (f *fixedAgentFinder) FindAgentsByRole(_ context.Context, role string) ([]string, error) {
	if a, ok := f.roleToAgent[role]; ok {
		return []string{a}, nil
	}
	return nil, nil
}

type conditionEval struct{}

func (c *conditionEval) EvaluateCondition(_ context.Context, _ string, facts map[string]interface{}) (bool, error) {
	if v, ok := facts["has_summary"]; ok {
		if s, ok := v.(string); ok && s == "true" {
			return true, nil
		}
	}
	return false, nil
}

// ---------------------------------------------------------------------------
// Scenario A: Sequential WorkflowExecutor
// ---------------------------------------------------------------------------

func runScenarioA(ctx context.Context, system *multiagent.AgentSystem) {
	fmt.Println("\n=== Scenario A: Sequential WorkflowExecutor ===")
	exec := multiagent.NewWorkflowExecutor(system).WithNodeExecutor(&researchNodeExecutor{}).WithMaxRetries(1)
	result, err := exec.Execute(ctx, "research-workflow", "climate impact")
	if err != nil {
		log.Fatalf("A failed: %v", err)
	}
	fmt.Printf("  Status: %s\n  Output: %v\n", result.Status, result.Output)
	for id, nr := range result.NodeResults {
		fmt.Printf("  - %-20s  agent=%-14s  output=%v\n", id, nr.AgentID, nr.Output)
	}
}

// ---------------------------------------------------------------------------
// Scenario B: ParallelWorkflowExecutor
// ---------------------------------------------------------------------------

func runScenarioB(ctx context.Context, system *multiagent.AgentSystem) {
	fmt.Println("\n=== Scenario B: ParallelWorkflowExecutor ===")
	wf, err := system.GetWorkflow(ctx, "research-workflow")
	if err != nil {
		log.Fatalf("B get workflow: %v", err)
	}
	exec := multiagent.NewParallelWorkflowExecutor(system).WithMaxParallel(2)
	result, err := exec.ExecuteParallel(ctx, wf, "parallel research")
	if err != nil {
		log.Fatalf("B failed: %v", err)
	}
	fmt.Printf("  Status: %s\n  Output: %v\n", result.Status, result.Output)
	for id, nr := range result.NodeResults {
		fmt.Printf("  - %-20s  agent=%-14s  output=%v\n", id, nr.AgentID, nr.Output)
	}
}

// ---------------------------------------------------------------------------
// Scenario C: HydratedWorkflowExecutor with session resume
// ---------------------------------------------------------------------------

func runScenarioC(ctx context.Context, system *multiagent.AgentSystem) {
	fmt.Println("\n=== Scenario C: HydratedWorkflowExecutor with Session Resume ===")
	loader := multiagent.NewDatalogWorkflowLoader(system)
	wfDef, err := loader.LoadWorkflow(ctx, "research-workflow")
	if err != nil {
		log.Fatalf("C loader: %v", err)
	}
	ss := newInMemorySessionStore()
	finder := &fixedAgentFinder{
		roleToAgent: map[string]string{
			"researcher": "researcher-a",
			"summarizer": "summarizer-1",
			"publisher":  "publisher-1",
		},
	}

	// C1: full pipeline run
	fmt.Println("  C.1 -- full pipeline...")
	h := multiagent.NewHydratedWorkflowExecutor(wfDef).
		WithConditionEvaluator(&conditionEval{}).
		WithAgentFinder(finder).
		WithNodeExecutor(&researchNodeExecutor{}).
		WithSessionStore(ss, "session-resume-001")
	res, inst, err := h.ExecuteWithSession(ctx, "fresh topic")
	if err != nil {
		log.Fatalf("C.1: %v", err)
	}
	fmt.Printf("    Status: %s | Instance: %s | Output: %v\n", res.Status, inst.Status, res.Output)
	fmt.Printf("    Completed: %v\n", inst.CompletedNodes)

	// C2: resume from a mid-flight session checkpoint
	fmt.Println("  C.2 -- resume mid-flight...")
	saved := &core.WorkflowInstance{
		WorkflowID:     "research-workflow",
		SessionID:      "session-resume-002",
		CurrentNodeID:  "summarize",
		Status:         core.WorkflowInstanceStatusRunning,
		Variables:      make(map[string]interface{}),
		Metadata:       make(map[string]string),
		LoopCounters:   make(map[string]int),
		CompletedNodes: []string{"research-a", "research-b"},
	}
	_ = ss.Create(ctx, saved)

	h2 := multiagent.NewHydratedWorkflowExecutor(wfDef).
		WithConditionEvaluator(&conditionEval{}).
		WithAgentFinder(finder).
		WithNodeExecutor(&researchNodeExecutor{}).
		WithSessionStore(ss, "session-resume-002")
	res2, inst2, err := h2.ExecuteWithSession(ctx, "resume")
	if err != nil {
		log.Fatalf("C.2: %v", err)
	}
	fmt.Printf("    Status: %s | Instance: %s | Output: %v\n", res2.Status, inst2.Status, res2.Output)
	fmt.Printf("    Completed: %v\n", inst2.CompletedNodes)
}

// ---------------------------------------------------------------------------
// Scenario D: Condition evaluation
// ---------------------------------------------------------------------------

func runScenarioD(ctx context.Context, system *multiagent.AgentSystem) {
	fmt.Println("\n=== Scenario D: Condition Evaluation ===")
	c1, _ := system.EvaluateCondition(ctx, `context("has_summary", "true")`, map[string]string{"has_summary": "true"})
	c2, _ := system.EvaluateCondition(ctx, `context("has_summary", "true")`, map[string]string{"has_summary": "false"})
	fmt.Printf("  has_summary=true  => %v (expected true)\n", c1)
	fmt.Printf("  has_summary=false => %v (expected false)\n", c2)
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

func main() {
	ctx := context.Background()
	fmt.Println("Multi-Agent Research Pipeline")
	fmt.Println("=============================")

	system, err := multiagent.NewAgentSystem(ctx)
	if err != nil {
		log.Fatalf("NewAgentSystem: %v", err)
	}
	if err := system.LoadAgentDefinitions(ctx); err != nil {
		log.Fatalf("LoadAgentDefinitions: %v", err)
	}
	custom, err := os.ReadFile(filepath.Join(exampleDir(), "research_agents.dlog"))
	if err != nil {
		log.Fatalf("read agents: %v", err)
	}
	if err := system.Engine().Runtime().AddPolicy(ctx, string(custom)); err != nil {
		log.Fatalf("AddPolicy: %v", err)
	}

	rs, _ := system.GetAgentsByRole(ctx, "researcher")
	ss, _ := system.GetAgentsByRole(ctx, "summarizer")
	ps, _ := system.GetAgentsByRole(ctx, "publisher")
	fmt.Printf("  Researchers: %d | Summarizers: %d | Publishers: %d\n", len(rs), len(ss), len(ps))

	wf, _ := system.GetWorkflow(ctx, "research-workflow")
	fmt.Printf("  Workflow: %s v%s (nodes=%d, edges=%d)\n", wf.Name, wf.Version, len(wf.Nodes), len(wf.Edges))
	for _, n := range wf.Nodes {
		fmt.Printf("    node %-20s type=%-10s agent=%s\n", n.ID, n.Type, n.Agent)
	}
	for _, e := range wf.Edges {
		c := ""
		if e.Condition != "" {
			c = fmt.Sprintf(" [cond: %s]", e.Condition)
		}
		fmt.Printf("    edge %s -> %s%s\n", e.From, e.To, c)
	}

	runScenarioA(ctx, system)
	runScenarioB(ctx, system)
	runScenarioC(ctx, system)
	runScenarioD(ctx, system)
	fmt.Println("\nComplete.")
}
