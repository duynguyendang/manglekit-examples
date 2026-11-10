% Deliberately BROKEN — unclosed body, used only by the fail-safe reload
% test. A reload attempt with this file must be rejected and the previous
% policy must keep serving (client.ReloadPolicy validates before swapping).

halt("Req", "this rule never closes"
