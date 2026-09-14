package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
	"transport-app/internal/shared"
)

func consentAuthedReq(t *testing.T, app *App, method, target, email string) *httptest.ResponseRecorder {
	t.Helper()
	user, err := app.Services.Users.GetUserByEmail(context.Background(), email)
	require.NoError(t, err)
	req := httptest.NewRequest(method, target, nil)
	ctx := context.WithValue(req.Context(), auth.ContextUser, &auth.SessionData{UserID: string(user.ID)})
	ctx = shared.ContextWithTenantID(ctx, shared.TenantID(user.TenantID))
	rr := httptest.NewRecorder()
	ah := NewAuthHandlers(app)
	switch target {
	case "/api/v1/consent":
		ah.ConsentStatusAPI(rr, req.WithContext(ctx))
	case "/api/v1/consent/withdraw":
		ah.WithdrawConsentAPI(rr, req.WithContext(ctx))
	case "/api/v1/consent/grant":
		ah.GrantConsentAPI(rr, req.WithContext(ctx))
	default:
		t.Fatalf("unknown consent target %s", target)
	}
	return rr
}

func TestConsentEndpoints_StatusWithdrawGrant(t *testing.T) {
	app := newRegisterTestApp(t)
	rr := postRegisterWithCompany(app, "Consent Web", "web@consent.test", "Str0ng!Pass", "Consent Web Co")
	require.Equal(t, http.StatusSeeOther, rr.Code)

	rr = consentAuthedReq(t, app, http.MethodGet, "/api/v1/consent", "web@consent.test")
	require.Equal(t, http.StatusOK, rr.Code)
	var status map[string]interface{}
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&status))
	require.Equal(t, true, status["granted"])
	require.Equal(t, false, status["withdrawn"])

	rr = consentAuthedReq(t, app, http.MethodPost, "/api/v1/consent/withdraw", "web@consent.test")
	require.Equal(t, http.StatusOK, rr.Code)

	rr = consentAuthedReq(t, app, http.MethodGet, "/api/v1/consent", "web@consent.test")
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&status))
	require.Equal(t, true, status["withdrawn"])

	rr = consentAuthedReq(t, app, http.MethodPost, "/api/v1/consent/grant", "web@consent.test")
	require.Equal(t, http.StatusOK, rr.Code)

	// Unauthenticated callers get 401, never consent state.
	anon := httptest.NewRequest(http.MethodGet, "/api/v1/consent", nil)
	anonRR := httptest.NewRecorder()
	NewAuthHandlers(app).ConsentStatusAPI(anonRR, anon)
	require.Equal(t, http.StatusUnauthorized, anonRR.Code)
}
