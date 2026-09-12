% real_llm_gate policy: the minimal supervised-LLM gate.
%
% One halt rule: the "answer" action may not run for the "weapons" topic.
% meta("topic", T) comes from sdk.WithMetadata("topic", T) on ExecuteByName.

halt("Req", "topic forbidden by policy: weapons") :-
    action_operation("Req", "answer"),
    meta("topic", "weapons").
