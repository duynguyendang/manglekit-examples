package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
