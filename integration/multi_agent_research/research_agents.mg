% ============================================================
% Agent Registry for Multi-Agent Research Workflow
% ============================================================

% --- Agent Roles ---
agent_role("researcher").
agent_role("summarizer").
agent_role("publisher").

% --- Role Capabilities ---
role_capability("researcher", "knowledge_retrieval").
role_capability("summarizer", "llm_generation").
role_capability("summarizer", "text_synthesis").
role_capability("publisher", "action_execution").

% --- Task Requirements ---
task_requires_capability("research_task", "knowledge_retrieval").
task_requires_capability("summarize_task", "text_synthesis").
task_requires_capability("publish_task", "action_execution").

% --- Agent Registry ---
agent("researcher-a", "researcher").
agent("researcher-b", "researcher").
agent("summarizer-1", "summarizer").
agent("publisher-1", "publisher").

% --- Agent Capabilities ---
agent_capability("researcher-a", "knowledge_retrieval").
agent_capability("researcher-b", "knowledge_retrieval").
agent_capability("summarizer-1", "llm_generation").
agent_capability("summarizer-1", "text_synthesis").
agent_capability("publisher-1", "action_execution").

% --- Agent Config ---
agent_config("researcher-a", "source", "web").
agent_config("researcher-b", "source", "documents").
agent_config("summarizer-1", "model", "mock-gpt").

% --- Agent Status ---
agent_status("researcher-a", "available").
agent_status("researcher-b", "available").
agent_status("summarizer-1", "available").
agent_status("publisher-1", "available").

% --- Workflow Definitions ---
workflow("research-workflow", "Research & Publish Pipeline", "v1.0").

% --- Workflow Nodes ---
workflow_node("research-workflow", "research-a",    "agent", "researcher").
workflow_node("research-workflow", "research-b",    "agent", "researcher").
workflow_node("research-workflow", "summarize",     "agent", "summarizer").
workflow_node("research-workflow", "publish",       "agent", "publisher").
workflow_node("research-workflow", "quality-gate",  "condition", "quality_check").

% --- Workflow Edges ---
workflow_edge("research-workflow", "research-a",    "summarize").
workflow_edge("research-workflow", "research-b",    "summarize").
workflow_edge("research-workflow", "summarize",     "quality-gate").
workflow_edge("research-workflow", "quality-gate",  "publish").

% --- Conditional Edges ---
% The quality gate checks that summarization produced a result.
% The condition is evaluated in Go by the conditionEval type.
conditional_edge("research-workflow", "quality-gate", "publish", "has_summary_true").
