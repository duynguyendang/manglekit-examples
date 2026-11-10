% ============================================================
% Goal-Based Planning Rules
% ============================================================
% This policy defines goal decomposition rules that break down
% high-level goals into ordered, executable plan steps.
% ============================================================

% --- Goal Decomposition: deploy_to_production ---

% Each goal decomposes into a sequence of ordered steps.
plan_step("check_tests", 1) :- goal("deploy_to_production").
plan_step("check_lint", 2) :- goal("deploy_to_production").
plan_step("deploy_staging", 3) :- goal("deploy_to_production").
plan_step("run_smoke_tests", 4) :- goal("deploy_to_production").
plan_step("deploy_production", 5) :- goal("deploy_to_production").

% --- Goal Decomposition: onboard_user ---

plan_step("create_account", 1) :- goal("onboard_user").
plan_step("assign_role", 2) :- goal("onboard_user").
plan_step("send_welcome", 3) :- goal("onboard_user").
plan_step("setup_mfa", 4) :- goal("onboard_user").

% --- Prerequisites ---

% Some steps have prerequisites that must be satisfied before execution.
requires("deploy_production", "approval").
requires("deploy_staging", "tests_pass").

% --- Policy Enforcement Rules ---

% Block deploy_production without approval.
halt("Req", "Production deploy requires approval") :-
    action_operation("Req", "deploy_production"),
    !meta("has_approval", "true").

% Block deploy_staging if tests haven't passed.
halt("Req", "Staging deploy requires passing tests") :-
    action_operation("Req", "deploy_staging"),
    !meta("tests_passed", "true").

% Block deploy_production during business hours (safety guard).
halt("Req", "Cannot deploy to production during business hours") :-
    action_operation("Req", "deploy_production"),
    meta("current_hour", "9").
halt("Req", "Cannot deploy to production during business hours") :-
    action_operation("Req", "deploy_production"),
    meta("current_hour", "10").
halt("Req", "Cannot deploy to production during business hours") :-
    action_operation("Req", "deploy_production"),
    meta("current_hour", "11").
halt("Req", "Cannot deploy to production during business hours") :-
    action_operation("Req", "deploy_production"),
    meta("current_hour", "12").
halt("Req", "Cannot deploy to production during business hours") :-
    action_operation("Req", "deploy_production"),
    meta("current_hour", "13").
halt("Req", "Cannot deploy to production during business hours") :-
    action_operation("Req", "deploy_production"),
    meta("current_hour", "14").
halt("Req", "Cannot deploy to production during business hours") :-
    action_operation("Req", "deploy_production"),
    meta("current_hour", "15").
halt("Req", "Cannot deploy to production during business hours") :-
    action_operation("Req", "deploy_production"),
    meta("current_hour", "16").
halt("Req", "Cannot deploy to production during business hours") :-
    action_operation("Req", "deploy_production"),
    meta("current_hour", "17").
