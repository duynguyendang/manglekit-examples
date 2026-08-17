% http_service policy: gate the /ask endpoint's supervised "answer" action.
%
% meta("topic", T) comes from sdk.WithMetadata("topic", req.Topic) in the
% handler; the supervisor pre-check evaluates this BEFORE the action runs,
% so a denied topic never reaches the LLM and the handler sees a policy
% violation it can map to HTTP 403.

halt("Req", "topic forbidden by policy: weapons") :-
    action_operation("Req", "answer"),
    meta("topic", "weapons").
