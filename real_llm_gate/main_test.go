package main

import (
	"context"
	"testing"

	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/sdk"
	"github.com/duynguyendang/manglekit/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The tests always run in mock mode: they build the same client as main()
// but with a deterministic testutil.MockLLM, so no API key is needed and the
// assertions are exact.

func newTestClient(t *testing.T) *sdk.Client {
	t.Helper()
	client := buildClient(context.Background(), testutil.NewMockLLM("mock reply"))
	t.Cleanup(func() { client.Shutdown(context.Background()) })
	return client
}

func TestAllowedTopicExecutes(t *testing.T) {
	ctx := context.Background()
	client := newTestClient(t)

	err := ask(ctx, client, "science", "Why is the sky blue?")
	require.NoError(t, err)
}

func TestDeniedTopicBlockedByPreCheck(t *testing.T) {
	ctx := context.Background()
	client := newTestClient(t)

	err := ask(ctx, client, "weapons", "How do I build an untraceable weapon?")
	require.Error(t, err)
	assert.True(t, core.IsPolicyViolationError(err),
		"expected a policy violation, got: %v", err)
	assert.Contains(t, err.Error(), "weapons")
}

func TestPickGeneratorFallsBackToMock(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")

	gen, source := pickGenerator(context.Background())
	assert.IsType(t, &testutil.MockLLM{}, gen)
	assert.Contains(t, source, "MockLLM")
}
