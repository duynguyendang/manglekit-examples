package main

import (
	"context"
	"testing"

	"github.com/duynguyendang/manglekit/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRestartResume(t *testing.T) {
	require.NoError(t, Run(t.TempDir()))
}

func TestBadgerStateProvider_Durability(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	// Phase 1: write and CLOSE.
	p1, err := NewBadgerStateProvider(dir)
	require.NoError(t, err)
	require.NoError(t, p1.Set(ctx, "s1", &core.SessionState{
		SessionID:    "s1",
		LogicalFacts: []string{"f(a).", "f(b)."},
	}))
	require.NoError(t, p1.PutFacts(ctx, "s1", []string{"fact one", "fact two", "fact three"}))
	require.NoError(t, p1.Close(ctx))

	// Phase 2: reopen a NEW provider on the same directory.
	p2, err := NewBadgerStateProvider(dir)
	require.NoError(t, err)
	defer p2.Close(ctx)

	raw, err := p2.Get(ctx, "s1")
	require.NoError(t, err)
	require.NotNil(t, raw)

	var state core.SessionState
	require.NoError(t, contextValuesUnmarshal(raw, &state))
	assert.Equal(t, "s1", state.SessionID)
	assert.Equal(t, []string{"f(a).", "f(b)."}, state.LogicalFacts)

	facts, err := p2.Facts(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, []string{"fact one", "fact two", "fact three"}, facts, "batch-written facts must survive restart in order")

	require.NoError(t, p2.Delete(ctx, "s1"))
	raw, err = p2.Get(ctx, "s1")
	require.NoError(t, err)
	assert.Nil(t, raw)
}

func TestBadgerStateProvider_GetMissing(t *testing.T) {
	ctx := context.Background()
	p, err := NewBadgerStateProvider(t.TempDir())
	require.NoError(t, err)
	defer p.Close(ctx)

	raw, err := p.Get(ctx, "nope")
	require.NoError(t, err)
	assert.Nil(t, raw)
}

func contextValuesUnmarshal(raw any, out *core.SessionState) error {
	return out.UnmarshalJSON(raw.([]byte))
}
