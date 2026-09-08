package taxverify

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDisabledByDefault(t *testing.T) {
	t.Setenv("TAXVERIFY_ENABLED", "")
	t.Setenv("TAXVERIFY_PROVIDER", "")

	p, err := New(ConfigFromEnv())
	require.NoError(t, err)
	require.NotNil(t, p, "seam must never hand out a nil provider")
	assert.Equal(t, "disabled", p.Name())

	_, err = p.VerifyGSTIN(context.Background(), "27AAPFU0939F1ZV")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotConfigured), "unconfigured lookup is unknown, never invalid")
}

func TestUnknownProviderIsConfigError(t *testing.T) {
	t.Setenv("TAXVERIFY_ENABLED", "1")
	t.Setenv("TAXVERIFY_PROVIDER", "nope")

	_, err := New(ConfigFromEnv())
	require.Error(t, err, "enabled-but-unknown provider must fail fast at wiring time")
}
