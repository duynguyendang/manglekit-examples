% ============================================================
% Fixed-Choice Routing Gate — the POLICY does the arithmetic
% ============================================================
% The Jev-style recipe keeps the confidence bars in Python. Here they are
% Datalog, AND the comparison itself is Datalog: the kernel publishes the
% model's per-class scores as integers (per-mille) and this program decides
% with :lt / :ge. No boolean is pre-computed in Go, so hot-swapping this file
% can change the decision LOGIC, not just the numbers.
%
% Why integers: Mangle's comparison builtins are integer-only and reject
% floats loudly ("value 0.9 is not a number"), so probabilities are published
% scaled by 1000 (scoring.PerMille). Truncation toward zero means a borderline
% score rounds DOWN and fails the bar — fail-closed by construction.
%
% Why rule-defined, not bare facts: ReloadPolicy swaps rules atomically, but
% bare facts survive the swap as base facts — a fact-defined bar would leave
% the old value queryable next to the new one. scoring.ThresholdsFromPolicy*
% treats that ambiguity as a hard error rather than picking one.

Decl policy_anchor(Marker).
policy_anchor("ok").

% --- The bars (per-mille integers) -------------------------------------
Decl threshold_pm(Name, Value).
threshold_pm("auto", 800) :- policy_anchor("ok").
threshold_pm("margin", 200) :- policy_anchor("ok").
threshold_pm("review", 700) :- policy_anchor("ok").

% --- Per-class bars -----------------------------------------------------
% A single global bar cannot say "account_access is riskier than billing".
% Ordinary classes inherit the global bar; the risk-bearing ones get their own
% — so the SAME score can be allowed for one class and held back for another.
%
% EVERY option the router can emit must have a bar. See the catch-all rule
% below for what happens when one is missing.
Decl class_bar(Option, Bar).
class_bar("billing", T) :- threshold_pm("auto", T).
class_bar("technical_support", T) :- threshold_pm("auto", T).
class_bar("account_access", 950) :- policy_anchor("ok").
class_bar("advance", T) :- threshold_pm("auto", T).
class_bar("interview", T) :- threshold_pm("auto", T).
class_bar("reject", T) :- threshold_pm("auto", T).
class_bar("approve", T) :- threshold_pm("auto", T).
class_bar("flag_audit", 950) :- policy_anchor("ok").
class_bar("reject_expense", T) :- threshold_pm("auto", T).

% Every option the router can emit must have a bar. has_class_bar/1 exists so
% the catch-all below can negate a UNARY, fully-bound atom — negating
% class_bar(C, B) with B unbound is a mode error at evaluation time.
Decl has_class_bar(Option).
has_class_bar(O) :- class_bar(O, B).
class_bar("advance", T) :- threshold_pm("auto", T).
class_bar("interview", T) :- threshold_pm("auto", T).
class_bar("reject", T) :- threshold_pm("auto", T).
class_bar("approve", T) :- threshold_pm("auto", T).
class_bar("flag_audit", 950) :- policy_anchor("ok").
class_bar("reject_expense", T) :- threshold_pm("auto", T).

% --- What the kernel publishes (facts on the request envelope) ----------
Decl choice(Option).
Decl score_pm(Option, Score).
Decl score_lcb_pm(Option, Score).
Decl margin_value_pm(Score).
Decl confidence(Value).
Decl margin_value(Value).
Decl entropy_value(Value).
Decl score_source(Source).
Decl truncated_top_k(Flag).

% --- T1: no distribution at all -> escalate -----------------------------
halt("Req", "unobserved_distribution_escalate", "T1") :-
    action_operation("Req", "route_ticket"),
    meta("unobserved_distribution", "true").

% --- T1: a choice with NO score (generation lane) -> review --------------
% The model wrote a letter and that is all the evidence there is. Publishing
% a fabricated 1.0 would clear every bar and produce an authoritative-looking
% proof about a number nobody measured, so no number is published at all and
% the policy gates on its ABSENCE.
halt("Req", "no_score_present_review", "T1") :-
    action_operation("Req", "route_ticket"),
    meta("no_score_present", "true").

% --- T1: nothing parsable came back -> escalate (OTHER/ESCALATE hatch) ---
halt("Req", "no_valid_choice_escalate", "T1") :-
    action_operation("Req", "route_ticket"),
    meta("no_valid_choice", "true").

% --- T1: an option with NO bar defined -> refuse -------------------------
% Fail-CLOSED by construction, and this rule exists because of a bug this
% policy actually shipped: when bars were defined only for the support domain,
% every screening/expense case auto-routed at ANY confidence, because the
% review rule could not join class_bar/2 and so never fired. Absence of a
% policy fact must never mean "allowed".
halt("Req", "no_class_bar_defined_refuse", "T1") :-
    action_operation("Req", "route_ticket"),
    choice(C), !has_class_bar(C).

% --- T1: below the review floor -> escalate to a human ------------------
halt("Req", "confidence_below_review_floor_escalate", "T1") :-
    action_operation("Req", "route_ticket"),
    choice(C), score_pm(C, P), threshold_pm("review", T), :lt(P, T).

% --- T1: below this class's bar -> send to review ------------------------
% Two comparisons make the bands mutually exclusive, so no negation (and no
% stratification question) is needed.
halt("Req", "low_confidence_send_to_review", "T1") :-
    action_operation("Req", "route_ticket"),
    choice(C), score_pm(C, P), class_bar(C, B), :lt(P, B),
    threshold_pm("review", T), :ge(P, T).

% --- T2 advisory: truncated evidence ------------------------------------
% score_pm already carries the sound LOWER bound when top_logprobs was
% truncated, so gating above is conservative by construction. This rule only
% makes the truncation visible in the proof tree; T2 never blocks.
halt("Req", "truncated top_logprobs — scores are lower bounds", "T2") :-
    action_operation("Req", "route_ticket"),
    truncated_top_k("true").

% --- T2 advisory: thin margin over the runner-up ------------------------
halt("Req", "thin margin over runner-up — consider spot-checking", "T2") :-
    action_operation("Req", "route_ticket"),
    choice(C), score_pm(C, P), class_bar(C, B), :ge(P, B),
    margin_value_pm(M), threshold_pm("margin", T), :lt(M, T).
