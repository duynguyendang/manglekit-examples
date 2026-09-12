package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv, err := NewServer(context.Background())
	require.NoError(t, err)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		ts.Close()
		srv.Shutdown(context.Background())
	})
	return ts
}

func postAsk(t *testing.T, ts *httptest.Server, body AskRequest) (int, map[string]any) {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	resp, err := http.Post(ts.URL+"/ask", "application/json", bytes.NewReader(raw))
	require.NoError(t, err)
	defer resp.Body.Close()
	var out map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	return resp.StatusCode, out
}

func TestAskAllowed(t *testing.T) {
	ts := newTestServer(t)

	code, body := postAsk(t, ts, AskRequest{Topic: "science", Query: "Why is the sky blue?"})
	assert.Equal(t, http.StatusOK, code)
	assert.Contains(t, body["reply"], "Mock answer")
}

func TestAskPolicyDenied(t *testing.T) {
	ts := newTestServer(t)

	code, body := postAsk(t, ts, AskRequest{Topic: "weapons", Query: "untraceable weapon?"})
	assert.Equal(t, http.StatusForbidden, code)
	msg, ok := body["error"].(string)
	require.True(t, ok, "error body must carry a message, got %v", body)
	assert.Contains(t, msg, "weapons")
}

func TestAskInvalidJSON(t *testing.T) {
	ts := newTestServer(t)

	resp, err := http.Post(ts.URL+"/ask", "application/json", bytes.NewReader([]byte("not json")))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// ============================================================================
// Hot policy reload (in-process; same guarantee as mkit serve + SIGHUP)
// ============================================================================

const allowWeaponsPolicy = `
halt("Req", "topic forbidden by policy: drugs") :-
    action_operation("Req", "answer"),
    meta("topic", "drugs").
`

func postReload(t *testing.T, ts *httptest.Server, path string) int {
	t.Helper()
	raw, _ := json.Marshal(ReloadRequest{PolicyPath: path})
	resp, err := http.Post(ts.URL+"/admin/reload", "application/json", bytes.NewReader(raw))
	require.NoError(t, err)
	defer resp.Body.Close()
	return resp.StatusCode
}

func TestHotReloadSwapsPolicy(t *testing.T) {
	ts := newTestServer(t)

	// Original policy: weapons denied, drugs allowed.
	code, _ := postAsk(t, ts, AskRequest{Topic: "weapons", Query: "q"})
	require.Equal(t, http.StatusForbidden, code)
	code, _ = postAsk(t, ts, AskRequest{Topic: "drugs", Query: "q"})
	require.Equal(t, http.StatusOK, code)

	// Write the new policy and reload.
	pol := filepath.Join(t.TempDir(), "policy2.dl")
	require.NoError(t, os.WriteFile(pol, []byte(allowWeaponsPolicy), 0o600))
	require.Equal(t, http.StatusOK, postReload(t, ts, pol))

	// Verdicts FLIPPED — same server, same client, zero restart.
	code, _ = postAsk(t, ts, AskRequest{Topic: "weapons", Query: "q"})
	require.Equal(t, http.StatusOK, code, "old rule must be gone after reload (whole user program swapped)")
	code, _ = postAsk(t, ts, AskRequest{Topic: "drugs", Query: "q"})
	require.Equal(t, http.StatusForbidden, code, "new rule must be live")

	// std.dl vocabulary still merged after the swap: the deny() alias → halt
	// derivation is an engine builtin and survives reloads.
	t.Run("builtins_survive", func(t *testing.T) {
		pol2 := filepath.Join(t.TempDir(), "policy3.dl")
		require.NoError(t, os.WriteFile(pol2, []byte(`
deny("Req", "topic forbidden by alias: gambling") :-
    action_operation("Req", "answer"),
    meta("topic", "gambling").
`), 0o600))
		require.Equal(t, http.StatusOK, postReload(t, ts, pol2))
		code, _ := postAsk(t, ts, AskRequest{Topic: "gambling", Query: "q"})
		require.Equal(t, http.StatusForbidden, code, "deny→halt std alias must still work post-reload")
		code, _ = postAsk(t, ts, AskRequest{Topic: "weapons", Query: "q"})
		require.Equal(t, http.StatusOK, code)
	})
}

func TestReloadFailKeepsOldPolicy(t *testing.T) {
	ts := newTestServer(t)

	bad := filepath.Join(t.TempDir(), "broken.dl")
	require.NoError(t, os.WriteFile(bad, []byte("halt((( not datalog"), 0o600))
	require.Equal(t, http.StatusBadRequest, postReload(t, ts, bad))

	// The rejected reload changed NOTHING: original policy still serving.
	code, _ := postAsk(t, ts, AskRequest{Topic: "weapons", Query: "q"})
	require.Equal(t, http.StatusForbidden, code)
}
