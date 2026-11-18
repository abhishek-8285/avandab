package auth

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/stretchr/testify/require"
)

// A matcher with an unknown function makes Enforce return an internal error.
// Today Can swallows it, so a genuine engine failure is indistinguishable
// from a normal policy deny and leaves no trace to debug.
func TestCasbinCan_LogsEnforcementErrorAndDenies(t *testing.T) {
	m, err := model.NewModelFromString(`
[request_definition]
r = sub, obj, act
[policy_definition]
p = sub, obj, act
[role_definition]
g = _, _
[policy_effect]
e = some(where (p.eft == allow))
[matchers]
m = undefinedFunc(r.sub) == r.obj
`)
	require.NoError(t, err)
	e, err := casbin.NewSyncedEnforcer(m)
	require.NoError(t, err)
	svc := &CasbinAuthorizationService{enforcer: e}

	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	require.False(t, svc.Can("user-1", "trips", "read"), "enforcement errors must fail closed")
	require.Contains(t, buf.String(), "casbin enforcement failed",
		"internal authorization errors must be logged, not silently collapsed into a deny")
	require.Contains(t, buf.String(), "Undefined function")
}
