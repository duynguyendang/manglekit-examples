% ============================================================
% Security Gate Policy for CI/CD & DevOps Operations
% ============================================================
% This policy enforces security and compliance rules on
% infrastructure operations before they are executed.
% ============================================================

% --- Security Rules ---

% Block open security groups (0.0.0.0/0)
halt("Req", "Security group allows unrestricted access (0.0.0.0/0)") :-
    action_operation("Req", "terraform_apply"),
    meta("has_open_security_group", "true").

% Block public database exposure
halt("Req", "Database must not be publicly accessible") :-
    action_operation("Req", "terraform_apply"),
    meta("db_publicly_accessible", "true").

% --- Approval Requirements ---

% Require approval for production deployments
halt("Req", "Production deployments require approval") :-
    action_operation("Req", "kubectl_deploy"),
    meta("target_env", "production"),
    !meta("has_approval", "true").

% Require approval for terraform destroy
halt("Req", "Terraform destroy requires approval") :-
    action_operation("Req", "terraform_destroy"),
    !meta("has_approval", "true").

% --- Business-Hours Window ---
%
% Cross-fact integer comparisons (:ge, :lt) evaluate business hours natively:
% Business hours are 09:00 to 18:00 (exclusive).
% Supports either direct integer facts: current_hour(H) / hour(H)
% or backward-compatible metadata: meta("business_hours", "true").

Decl current_hour(H).
Decl target_replicas(N).

halt("Req", "Cannot destroy infrastructure during business hours") :-
    action_operation("Req", "terraform_destroy"),
    current_hour(H),
    :ge(H, 9),
    :lt(H, 18).

halt("Req", "Cannot destroy infrastructure during business hours") :-
    action_operation("Req", "terraform_destroy"),
    meta("business_hours", "true").

% --- Replica Limit ---
%
% Native Mangle integer comparison: max 10 replicas, min 1 replica.
% Matches either direct target_replicas(N) facts or backward-compatible
% precomputed meta flags.

halt("Req", "Scale exceeds maximum replicas") :-
    action_operation("Req", "kubectl_scale"),
    target_replicas(N),
    :gt(N, 10).

halt("Req", "Scale exceeds maximum replicas") :-
    action_operation("Req", "kubectl_scale"),
    meta("scale_too_high", "true").

halt("Req", "Cannot scale to zero replicas") :-
    action_operation("Req", "kubectl_scale"),
    target_replicas(N),
    :lt(N, 1).

halt("Req", "Cannot scale to zero replicas") :-
    action_operation("Req", "kubectl_scale"),
    meta("scale_too_low", "true").

% --- Tier-attributed rules (explicit halt/3 tiers, resolved P0.7 semantics) ---
%
% The third halt argument carries the governance tier. The supervisor's gate
% blocks only on T0/T1; explicit T2/T3 rules are ADVISORY (the action runs).
% Tier-less halts (all rules above) keep the fail-closed default: block.

% T1 governance: production deployments need a rollback plan — BLOCKS.
halt("Req", "Production deployments require a rollback plan", "T1") :-
    action_operation("Req", "kubectl_deploy"),
    meta("target_env", "production"),
    !meta("has_rollback_plan", "true").

% T2 playbook: an explicit negative flag (not absence) so only opted-in
% scenarios trip it — this rule is VISIBLE in Explain but NEVER blocks.
halt("Req", "deploy without canary rollout is discouraged by the release playbook", "T2") :-
    action_operation("Req", "kubectl_deploy"),
    meta("has_canary", "false").
