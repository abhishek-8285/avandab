package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
	"transport-app/internal/shared"
)

// consentPageReq builds an authenticated page request for the consent notice
// handlers (session identity + tenant context, mirroring consentAuthedReq).
func consentPageReq(t *testing.T, app *App, method, target, email string, form url.Values) (*httptest.ResponseRecorder, *http.Request) {
	t.Helper()
	user, err := app.Services.Users.GetUserByEmail(context.Background(), email)
	require.NoError(t, err)
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	req := httptest.NewRequest(method, target, body)
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	ctx := context.WithValue(req.Context(), auth.ContextUser, &auth.SessionData{UserID: string(user.ID)})
	ctx = shared.ContextWithTenantID(ctx, shared.TenantID(user.TenantID))
	return httptest.NewRecorder(), req.WithContext(ctx)
}

func TestConsentNoticePage_RendersNotice(t *testing.T) {
	app := newRegisterTestApp(t)
	rr := postRegisterWithCompany(app, "Notice Web", "notice@consent.test", "Str0ng!Pass", "Notice Co")
	require.Equal(t, http.StatusSeeOther, rr.Code)

	w, req := consentPageReq(t, app, http.MethodGet, "/consent", "notice@consent.test", nil)
	NewAuthHandlers(app).ConsentNoticePage(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "Your data — your consent")
	assert.Contains(t, body, "Digital Personal Data Protection Act, 2023")
	assert.Contains(t, body, "account identity and contact details")
	assert.Contains(t, body, "operate your account on this platform")
	assert.Contains(t, body, "POST /api/v1/consent/withdraw")
	assert.Contains(t, body, "will not be able to log in until you grant consent again")
	assert.Contains(t, body, "/contact-us")
	assert.Contains(t, body, "Notice version:")
	assert.Contains(t, body, "v1")
	assert.Contains(t, body, `name="agree"`)
	assert.Contains(t, body, "Withdraw consent")
}

func TestConsentNoticePage_UnauthenticatedRedirectsLogin(t *testing.T) {
	app := newRegisterTestApp(t)
	req := httptest.NewRequest(http.MethodGet, "/consent", nil)
	w := httptest.NewRecorder()
	NewAuthHandlers(app).ConsentNoticePage(w, req)
	assert.Equal(t, http.StatusSeeOther, w.Code)
	assert.Equal(t, "/login", w.Header().Get("Location"))
}

func TestConsentGrantForm_CheckboxRequired(t *testing.T) {
	app := newRegisterTestApp(t)
	rr := postRegisterWithCompany(app, "Checkbox Web", "checkbox@consent.test", "Str0ng!Pass", "Checkbox Co")
	require.Equal(t, http.StatusSeeOther, rr.Code)

	// Withdraw first so a silent grant would be observable in the ledger.
	w, req := consentPageReq(t, app, http.MethodPost, "/consent", "checkbox@consent.test", url.Values{"action": {"withdraw"}})
	ah := NewAuthHandlers(app)
	ah.ConsentGrantForm(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// Missing checkbox: re-render with error, withdrawal must stand.
	w, req = consentPageReq(t, app, http.MethodPost, "/consent", "checkbox@consent.test", url.Values{})
	ah.ConsentGrantForm(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "explicit agreement")

	user, err := app.Services.Users.GetUserByEmail(context.Background(), "checkbox@consent.test")
	require.NoError(t, err)
	_, withdrawnAt, err := app.Services.Users.ConsentStatus(context.Background(), user.TenantID, string(user.ID))
	require.NoError(t, err)
	assert.True(t, withdrawnAt.Valid, "post without agree=yes must not grant consent")

	// Wrong value is not agreement either.
	w, req = consentPageReq(t, app, http.MethodPost, "/consent", "checkbox@consent.test", url.Values{"agree": {"on"}})
	ah.ConsentGrantForm(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	_, withdrawnAt, err = app.Services.Users.ConsentStatus(context.Background(), user.TenantID, string(user.ID))
	require.NoError(t, err)
	assert.True(t, withdrawnAt.Valid, "agree=on must not grant consent")

	// Explicit checkbox grants and lands on the dashboard.
	w, req = consentPageReq(t, app, http.MethodPost, "/consent", "checkbox@consent.test", url.Values{"agree": {"yes"}})
	ah.ConsentGrantForm(w, req)
	assert.Equal(t, http.StatusSeeOther, w.Code)
	assert.Equal(t, "/dashboard", w.Header().Get("Location"))
	grantedAt, withdrawnAt, err := app.Services.Users.ConsentStatus(context.Background(), user.TenantID, string(user.ID))
	require.NoError(t, err)
	assert.True(t, grantedAt.Valid)
	assert.False(t, withdrawnAt.Valid, "explicit grant must clear withdrawal")
}

func TestConsentGrantForm_WithdrawFromPage(t *testing.T) {
	app := newRegisterTestApp(t)
	rr := postRegisterWithCompany(app, "Withdraw Web", "withdrawpage@consent.test", "Str0ng!Pass", "Withdraw Co")
	require.Equal(t, http.StatusSeeOther, rr.Code)

	w, req := consentPageReq(t, app, http.MethodPost, "/consent", "withdrawpage@consent.test", url.Values{"action": {"withdraw"}})
	NewAuthHandlers(app).ConsentGrantForm(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "Consent withdrawn")

	user, err := app.Services.Users.GetUserByEmail(context.Background(), "withdrawpage@consent.test")
	require.NoError(t, err)
	_, withdrawnAt, err := app.Services.Users.ConsentStatus(context.Background(), user.TenantID, string(user.ID))
	require.NoError(t, err)
	assert.True(t, withdrawnAt.Valid)
}

func TestConsentGrantForm_UnauthenticatedRedirectsLogin(t *testing.T) {
	app := newRegisterTestApp(t)
	form := url.Values{"agree": {"yes"}}
	req := httptest.NewRequest(http.MethodPost, "/consent", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	NewAuthHandlers(app).ConsentGrantForm(w, req)
	assert.Equal(t, http.StatusSeeOther, w.Code)
	assert.Equal(t, "/login", w.Header().Get("Location"))
}
