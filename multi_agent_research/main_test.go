package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/multiagent"
)

func setup(t *testing.T, ctx context.Context) *multiagent.AgentSystem {
	t.Helper()
	system, err := multiagent.NewAgentSystem(ctx)
	if err != nil {
		t.Fatalf("NewAgentSystem: %v", err)
	}
	if err := system.LoadAgentDefinitions(ctx); err != nil {
		t.Fatalf("LoadAgentDefinitions: %v", err)
	}
	custom, err := os.ReadFile(filepath.Join(exampleDir(), "research_agents.dlog"))
	if err != nil {
		t.Fatalf("read research_agents.dlog: %v", err)
	}
	if err := system.Engine().Runtime().AddPolicy(ctx, string(custom)); err != nil {
		t.Fatalf("AddPolicy: %v", err)
	}
	return system
}

func TestAgentSystemLoadsResearchAgents(t *testing.T) {
	ctx := context.Background()
	system := setup(t, ctx)
	agents, err := system.GetAgentsByRole(ctx, "researcher")
	if err != nil {
		t.Fatalf("GetAgentsByRole: %v", err)
	}
	if len(agents) < 2 {
		t.Errorf("expected >=2 researchers, got %d", len(agents))
	}
}

func TestWorkflowExecutor(t *testing.T) {
	ctx := context.Background()
	system := setup(t, ctx)
	exec := multiagent.NewWorkflowExecutor(system).
		WithNodeExecutor(&researchNodeExecutor{}).
		WithMaxRetries(1)
	result, err := exec.Execute(ctx, "research-workflow", "test input")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != multiagent.WorkflowStatusCompleted {
		t.Errorf("expected completed, got %s", result.Status)
	}
	if len(result.NodeResults) == 0 {
		t.Error("expected node results")
	}
}

func TestParallelWorkflowExecutor(t *testing.T) {
	ctx := context.Background()
	system := setup(t, ctx)
	wf, err := system.GetWorkflow(ctx, "research-workflow")
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	exec := multiagent.NewParallelWorkflowExecutor(system).WithMaxParallel(2)
	result, err := exec.ExecuteParallel(ctx, wf, "parallel test")
	if err != nil {
		t.Fatalf("ExecuteParallel: %v", err)
	}
	if result.Status != multiagent.WorkflowStatusCompleted {
		t.Errorf("expected completed, got %s", result.Status)
	}
}

func TestHydratedWorkflowExecutor(t *testing.T) {
	ctx := context.Background()
	system := setup(t, ctx)
	loader := multiagent.NewDatalogWorkflowLoader(system)
	wfDef, err := loader.LoadWorkflow(ctx, "research-workflow")
	if err != nil {
		t.Fatalf("LoadWorkflow: %v", err)
	}
	ss := newInMemorySessionStore()
	finder := &fixedAgentFinder{
		roleToAgent: map[string]string{
			"researcher": "researcher-a",
			"summarizer": "summarizer-1",
			"publisher":  "publisher-1",
		},
	}
	h := multiagent.NewHydratedWorkflowExecutor(wfDef).
		WithConditionEvaluator(&conditionEval{}).
		WithAgentFinder(finder).
		WithNodeExecutor(&researchNodeExecutor{}).
		WithSessionStore(ss, "test-session-001")
	res, inst, err := h.ExecuteWithSession(ctx, "test topic")
	if err != nil {
		t.Fatalf("ExecuteWithSession: %v", err)
	}
	if res.Status != core.WorkflowStatusCompleted {
		t.Errorf("expected completed, got %s", res.Status)
	}
	if inst.Status != core.WorkflowInstanceStatusCompleted {
		t.Errorf("expected instance completed, got %s", inst.Status)
	}
	if len(inst.CompletedNodes) == 0 {
		t.Error("expected completed nodes")
	}
}

func TestSessionResume(t *testing.T) {
	ctx := context.Background()
	system := setup(t, ctx)
	loader := multiagent.NewDatalogWorkflowLoader(system)
	wfDef, err := loader.LoadWorkflow(ctx, "research-workflow")
	if err != nil {
		t.Fatalf("LoadWorkflow: %v", err)
	}
	ss := newInMemorySessionStore()
	finder := &fixedAgentFinder{
		roleToAgent: map[string]string{
			"researcher": "researcher-a",
			"summarizer": "summarizer-1",
			"publisher":  "publisher-1",
		},
	}

	saved := &core.WorkflowInstance{
		WorkflowID:     "research-workflow",
		SessionID:      "resume-test-001",
		CurrentNodeID:  "summarize",
		Status:         core.WorkflowInstanceStatusRunning,
		Variables:      make(map[string]interface{}),
		Metadata:       make(map[string]string),
		LoopCounters:   make(map[string]int),
		CompletedNodes: []string{"research-a", "research-b"},
	}
	_ = ss.Create(ctx, saved)

	h := multiagent.NewHydratedWorkflowExecutor(wfDef).
		WithConditionEvaluator(&conditionEval{}).
		WithAgentFinder(finder).
		WithNodeExecutor(&researchNodeExecutor{}).
		WithSessionStore(ss, "resume-test-001")
	res, inst, err := h.ExecuteWithSession(ctx, "resume")
	if err != nil {
		t.Fatalf("ExecuteWithSession: %v", err)
	}
	if res.Status != core.WorkflowStatusCompleted {
		t.Errorf("expected completed, got %s", res.Status)
	}
	if len(inst.CompletedNodes) < 3 {
		t.Errorf("expected >=3 completed nodes after resume, got %d: %v",
			len(inst.CompletedNodes), inst.CompletedNodes)
	}
}

func TestConditionEvaluation(t *testing.T) {
	ctx := context.Background()
	system := setup(t, ctx)
	c1, _ := system.EvaluateCondition(ctx,
		`context("has_summary", "true")`, map[string]string{"has_summary": "true"})
	if !c1 {
		t.Error("expected true for has_summary=true")
	}
	c2, _ := system.EvaluateCondition(ctx,
		`context("has_summary", "true")`, map[string]string{"has_summary": "false"})
	if c2 {
		t.Error("expected false for has_summary=false")
	}
}

func TestGetWorkflowNodes(t *testing.T) {
	ctx := context.Background()
	system := setup(t, ctx)
	wf, err := system.GetWorkflow(ctx, "research-workflow")
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if len(wf.Nodes) != 5 {
		t.Errorf("expected 5 nodes, got %d", len(wf.Nodes))
	}
	if len(wf.Edges) != 5 {
		t.Errorf("expected 5 edges, got %d", len(wf.Edges))
	}
}
