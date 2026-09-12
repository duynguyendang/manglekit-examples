% GDPR compliance proof: machine-checkable regulation as tiered Datalog.
%
% Tier mapping:
%   T0 (Axiom)   — Art.9 special-category data: biometric, health, racial origin
%   T1 (Govern)  — Art.6 lawful basis: consent, contract, legitimate_interest
%
% Security labels (injected via SecurityLabels) become label("tag") facts.
% env.Facts are injected directly as Datalog facts.
%
% Each halt rule carries a tier annotation so the AuditTrail renders
% the exact regulation invoked, the tier, and variable bindings.

% --- T0: Art.9 special-category data (hard prohibition) ---

Decl purpose(Purpose).
Decl consent(User).
Decl has_lawful_basis(Basis).

halt(Req, "Art.9: special-category data requires explicit consent") :-
    action_operation(Req, "process_data"),
    label("special"),
    !consent("explicit").

% --- T1: Art.6 lawful basis (must have at least one) ---

halt(Req, "Art.6: no lawful basis for processing") :-
    action_operation(Req, "process_data"),
    label("personal"),
    !has_lawful_basis("consent"),
    !has_lawful_basis("contract"),
    !has_lawful_basis("legitimate_interest").
