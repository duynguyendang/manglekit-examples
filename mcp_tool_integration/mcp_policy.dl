% ============================================================
% MCP Tool Integration Policy
% ============================================================
% This policy governs MCP (Model Context Protocol) tool execution.
% It enforces path-based access control and operation restrictions
% on tools discovered from MCP servers.
% ============================================================

% --- Sensitive Path Protection ---

% Block write operations to sensitive system paths (/etc, /sys, /proc).
% Reads are allowed; only writes are forbidden at this layer.
halt("Req", "Write operations to sensitive system paths are forbidden") :-
    meta("mcp_operation", "write"),
    meta("mcp_path_sensitive", "true").

% Block ALL operations (read, write, delete) on restricted paths.
% This is a strictly broader rule than the write-only one above and
% is intentional: reading restricted paths can still leak credentials,
% kernel state, or device descriptors, so the broader gate is safer
% than the narrower one.
halt("Req", "Access to restricted paths is blocked") :-
    meta("mcp_path_restricted", "true").

% --- Operation Restrictions ---

% Block all delete operations (safety net)
halt("Req", "Delete operations are not permitted via MCP tools") :-
    meta("mcp_operation", "delete").

