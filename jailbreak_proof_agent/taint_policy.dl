% Jailbreak-proof agent: T0 axiom blocks egress of tainted data.
%
% Security model:
%   - label("tainted") is injected via SecurityLabels when the agent
%     reads from an untrusted document.
%   - The T0 axiom below fires on ANY egress action carrying that label,
%     regardless of what the LLM output says.
%   - A clean-document run never gets label("tainted"), so the gate is
%     precise (not blanket-deny).

% --- T0: data-exfil axiom (kernel-level hard law) ---

Decl egress_action(Action).

egress_action("send_email").
egress_action("http_post").
egress_action("file_write").

halt(Req, "T0: egress blocked — payload carries tainted data") :-
    action_operation(Req, Op),
    egress_action(Op),
    label("tainted").
