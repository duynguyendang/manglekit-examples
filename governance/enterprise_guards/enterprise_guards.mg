% --- Enterprise Guard Policies ---

% =============================================================================
% 1. Financial Refund Guard
% =============================================================================
% Gating financial disbursements:
% - Refunds > $1,000 (100,000 cents) require dual approval ("manager_approval").
% - Rerouting refund to an unverified destination account is unconditionally blocked.
% =============================================================================

Decl amount_cents(Req, Cents).
Decl destination_account(Req, Account).
Decl account_verified(Account).
Decl manager_approval(Req, Approver).
Decl has_manager_approval(Req).

has_manager_approval(Req) :- manager_approval(Req, _).

% Base trusted accounts
account_verified("acc_verified_123").

% Halt: refund amount exceeds threshold without manager approval
halt("Req", "refund exceeds limit without manager approval", "T1") :-
    action_operation("Req", "process_refund"),
    amount_cents("Req", Amount),
    :gt(Amount, 100000),
    !has_manager_approval("Req").

% Halt: refund destination account is not verified (account takeover risk)
halt("Req", "refund destination account is unverified", "T1") :-
    action_operation("Req", "process_refund"),
    destination_account("Req", Acc),
    !account_verified(Acc).

% =============================================================================
% 2. Destructive Resource Delete Guard
% =============================================================================
% Gating destructive operations (DROP table, delete cluster, terraform destroy):
% - Requires verified owner identity.
% - Production environments require a valid break-glass approval token.
% =============================================================================

Decl resource_env(Req, Env).
Decl requester(Req, User).
Decl is_owner(User, Resource).
Decl target_resource(Req, Resource).
Decl breakglass_token(Req, Token).
Decl has_breakglass_token(Req).

has_breakglass_token(Req) :- breakglass_token(Req, _).

% Base ownership facts
is_owner("alice", "db-prod-primary").

% Halt: destructive delete attempted by non-owner
halt("Req", "requester is not resource owner", "T1") :-
    action_operation("Req", "delete_resource"),
    requester("Req", User),
    target_resource("Req", Res),
    !is_owner(User, Res).

% Halt: production deletion without breakglass token
halt("Req", "production deletion requires valid breakglass token", "T1") :-
    action_operation("Req", "delete_resource"),
    resource_env("Req", "production"),
    !has_breakglass_token("Req").

% =============================================================================
% 3. Communication & OTP Leak Guard
% =============================================================================
% Gating outbound messaging tools (SMS/Email dispatch):
% - Sending one-time authentication codes (OTP) to unverified phones is blocked.
% - Dispatching classified PII outside trusted channels is blocked.
% =============================================================================

Decl message_type(Req, Type).
Decl recipient_phone(Req, Phone).
Decl phone_verified(Phone).
Decl contains_pii(Req, Flag).
Decl channel_secure(Req, Flag).

% Base verified phone numbers
phone_verified("+15550199999").

% Halt: sending OTP code to an unverified phone number
halt("Req", "cannot send auth code to unverified recipient phone", "T1") :-
    action_operation("Req", "send_message"),
    message_type("Req", "otp_auth_code"),
    recipient_phone("Req", Phone),
    !phone_verified(Phone).

% Halt: sending PII over an unencrypted/insecure channel
halt("Req", "dispatching PII over insecure channel is blocked", "T1") :-
    action_operation("Req", "send_message"),
    contains_pii("Req", "true"),
    !channel_secure("Req", "true").
