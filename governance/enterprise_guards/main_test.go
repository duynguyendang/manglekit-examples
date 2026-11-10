package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/duynguyendang/manglekit"
	"github.com/duynguyendang/manglekit/adapters/func"
	"github.com/duynguyendang/manglekit/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestClient(t *testing.T) *manglekit.Client {
	t.Helper()
	ctx := context.Background()

	policyPath := filepath.Join(exampleDir(), "enterprise_guards.mg")
	policyData := manglekit.MustReadFile(policyPath)

	client, err := manglekit.NewClient(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Shutdown(ctx) })

	require.NoError(t, client.LoadPolicy(ctx, string(policyData)))

	client.RegisterSupervised("process_refund", function.New("process_refund",
		func(ctx context.Context, in RefundRequest) (string, error) {
			return "refund_ok", nil
		}))

	client.RegisterSupervised("delete_resource", function.New("delete_resource",
		func(ctx context.Context, in DeleteRequest) (string, error) {
			return "delete_ok", nil
		}))

	client.RegisterSupervised("send_message", function.New("send_message",
		func(ctx context.Context, in MessageRequest) (string, error) {
			return "message_ok", nil
		}))

	return client
}

func TestFinancialRefundGuard(t *testing.T) {
	client := setupTestClient(t)
	ctx := context.Background()

	t.Run("refund_exceeds_threshold_blocked", func(t *testing.T) {
		_, err := client.ExecuteByName(ctx, "process_refund", RefundRequest{
			AmountCents: 150000,
			Destination: "acc_verified_123",
		})
		require.Error(t, err)
		assert.True(t, core.IsPolicyViolationError(err))
		assert.Contains(t, err.Error(), "refund exceeds limit without manager approval")
	})

	t.Run("unverified_destination_blocked", func(t *testing.T) {
		_, err := client.ExecuteByName(ctx, "process_refund", RefundRequest{
			AmountCents: 5000,
			Destination: "acc_fraudulent_999",
		})
		require.Error(t, err)
		assert.True(t, core.IsPolicyViolationError(err))
		assert.Contains(t, err.Error(), "refund destination account is unverified")
	})

	t.Run("refund_with_approval_allowed", func(t *testing.T) {
		env := core.NewEnvelope(RefundRequest{
			AmountCents: 150000,
			Destination: "acc_verified_123",
		})
		env.Facts = []string{`manager_approval("Req", "mgr_bob").`}
		res, err := client.ExecuteByName(ctx, "process_refund", env)
		require.NoError(t, err)
		assert.Equal(t, "refund_ok", res.Payload)
	})
}

func TestDestructiveDeleteGuard(t *testing.T) {
	client := setupTestClient(t)
	ctx := context.Background()

	t.Run("non_owner_blocked", func(t *testing.T) {
		_, err := client.ExecuteByName(ctx, "delete_resource", DeleteRequest{
			Resource: "db-prod-primary",
			Env:      "staging",
			User:     "mallory",
		})
		require.Error(t, err)
		assert.True(t, core.IsPolicyViolationError(err))
		assert.Contains(t, err.Error(), "requester is not resource owner")
	})

	t.Run("production_without_breakglass_blocked", func(t *testing.T) {
		_, err := client.ExecuteByName(ctx, "delete_resource", DeleteRequest{
			Resource: "db-prod-primary",
			Env:      "production",
			User:     "alice",
		})
		require.Error(t, err)
		assert.True(t, core.IsPolicyViolationError(err))
		assert.Contains(t, err.Error(), "production deletion requires valid breakglass token")
	})

	t.Run("production_with_breakglass_allowed", func(t *testing.T) {
		env := core.NewEnvelope(DeleteRequest{
			Resource: "db-prod-primary",
			Env:      "production",
			User:     "alice",
		})
		env.Facts = []string{`breakglass_token("Req", "token_xyz").`}
		res, err := client.ExecuteByName(ctx, "delete_resource", env)
		require.NoError(t, err)
		assert.Equal(t, "delete_ok", res.Payload)
	})
}

func TestCommunicationOTPGuard(t *testing.T) {
	client := setupTestClient(t)
	ctx := context.Background()

	t.Run("otp_to_unverified_phone_blocked", func(t *testing.T) {
		_, err := client.ExecuteByName(ctx, "send_message", MessageRequest{
			Type:      "otp_auth_code",
			Recipient: "+15550001111",
		})
		require.Error(t, err)
		assert.True(t, core.IsPolicyViolationError(err))
		assert.Contains(t, err.Error(), "cannot send auth code to unverified recipient phone")
	})

	t.Run("otp_to_verified_phone_allowed", func(t *testing.T) {
		res, err := client.ExecuteByName(ctx, "send_message", MessageRequest{
			Type:      "otp_auth_code",
			Recipient: "+15550199999",
		})
		require.NoError(t, err)
		assert.Equal(t, "message_ok", res.Payload)
	})
}
