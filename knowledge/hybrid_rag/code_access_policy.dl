% ============================================
% Multi-Tenant Code Repository Access Policy
% ============================================
% Enforces transitive access control:
% User → Team → Repo → Module
%
% Knowledge Graph (loaded as triple facts):
%   triple(User, "member_of", Team)
%   triple(Team, "contributor_to", Repo)
%   triple(Repo, "contains_module", Module)
%   triple(Module, "has_label", Label)
% ============================================

% Transitive access: User can access Module if:
% - User is a member of a Team
% - Team is a contributor to a Repo
% - Repo contains the Module
can_access(User, Module) :-
    triple(User, "member_of", Team),
    triple(Team, "contributor_to", Repo),
    triple(Repo, "contains_module", Module).

% Module → containing Repo: derive a positive fact for every
% module in the graph. This gives us a "module exists in some
% repo" predicate that the halt rule can check directly.
module_repo(Module, Repo) :-
    triple(Repo, "contains_module", Module).

% User → reachable Repo: derive a positive fact for every
% (User, Repo) pair where the user can reach the repo via any
% team. Again, positive derivation so the halt rule can check
% membership directly.
user_repo(User, Repo) :-
    triple(User, "member_of", Team),
    triple(Team, "contributor_to", Repo).

% Enforcement: Block if the user cannot reach the repo that
% contains the target module. Both arguments to the lookup are
% bound from meta() and the positive derivations, so the negation
% fires reliably per-(User, TargetModule) pair.
halt("Req", "Access denied: user cannot access this module") :-
    action_operation("Req", "search_code"),
    meta("query_user", User),
    meta("target_module", TargetModule),
    module_repo(TargetModule, Repo),
    !user_repo(User, Repo).

% Allow if no halt triggered
allow("Req") :-
    action_operation("Req", "search_code"),
    !halt("Req", _).
