package main

import (
	"context"
	"testing"
	"time"

	"github.com/duynguyendang/manglekit"
	"github.com/stretchr/testify/require"
)

func mustClient(t *testing.T, ctx context.Context) *manglekit.Client {
	t.Helper()
	client, err := manglekit.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestValidityAtTransactionTime(t *testing.T) {
	ctx := context.Background()
	client := mustClient(t, ctx)
	defer client.Shutdown(ctx)

	require.NoError(t, client.EnableTemporal())
	on, err := client.IsTemporalEnabled()
	require.NoError(t, err)
	require.True(t, on)

	jan := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	janEnd := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	mar := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)

	require.NoError(t, client.AddEternalFact(`framework("sox_compliance")`))

	// window facts, exactly as the example
	require.NoError(t, client.AddTemporalFact(`authorized("auth_january")`, jan, janEnd))
	require.NoError(t, client.AddTemporalFact(`authorized("rollover_feb")`, jan, mar))

	// Because the admin factor is eternal, the eternal predicate must be in the store.
	t.Run("valid inside window", func(t *testing.T) {
		inside := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)
		require.NoError(t, client.SetEvaluationTime(inside))
		ok, err := client.ContainsTemporalFact(`authorized("auth_january")`)
		require.NoError(t, err)
		require.True(t, ok, "authorization must be in effect inside its window")
	})

	t.Run("invalid after window", func(t *testing.T) {
		require.NoError(t, client.SetEvaluationTime(mar))
		ok, err := client.ContainsTemporalFact(`authorized("auth_january")`)
		require.NoError(t, err)
		require.False(t, ok, "authorization must expire after its window")
	})

	t.Run("query during returns only matching window", func(t *testing.T) {
		found, err := client.QueryTemporalFactsDuring(`authorized(ID)`, jan, janEnd)
		require.NoError(t, err)
		require.NotEmpty(t, found)
	})

	t.Run("query at point in time", func(t *testing.T) {
		mid := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)
		found, err := client.QueryTemporalFactsAt(`authorized(ID)`, mid)
		require.NoError(t, err)
		require.True(t, contains(found, `authorized("rollover_feb")`))
		require.False(t, contains(found, `authorized("auth_january")`))
	})
}

func contains(hay []string, needle string) bool {
	for _, s := range hay {
		if s == needle {
			return true
		}
	}
	return false
}