package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/domain"
)

type mockCompanySettingsReader struct {
	settings domain.CompanySettings
	err      error
}

func (m *mockCompanySettingsReader) GetSettings(ctx context.Context) (domain.CompanySettings, error) {
	if m.err != nil {
		return domain.CompanySettings{}, m.err
	}
	return m.settings, nil
}

func strPtr(s string) *string {
	return &s
}

func TestRequireCompanyCompliance(t *testing.T) {
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	tests := []struct {
		name           string
		path           string
		reader         CompanySettingsReader
		expectedStatus int
		expectRedirect bool
		expectCookie   bool
	}{
		{
			name: "incomplete company settings - redirects to onboard",
			path: "/dashboard",
			reader: &mockCompanySettingsReader{
				settings: domain.CompanySettings{
					CompanyName: "",
				},
			},
			expectedStatus: http.StatusSeeOther,
			expectRedirect: true,
			expectCookie:   true,
		},
		{
			name: "missing address - redirects to onboard",
			path: "/vehicles",
			reader: &mockCompanySettingsReader{
				settings: domain.CompanySettings{
					CompanyName: "Acme Logistics",
					Address:     nil,
					Phone:       strPtr("+91 9999999999"),
					Email:       strPtr("ops@acme.test"),
				},
			},
			expectedStatus: http.StatusSeeOther,
			expectRedirect: true,
			expectCookie:   true,
		},
		{
			name: "empty address string - redirects to onboard",
			path: "/trips",
			reader: &mockCompanySettingsReader{
				settings: domain.CompanySettings{
					CompanyName: "Acme Logistics",
					Address:     strPtr("   "),
					Phone:       strPtr("+91 9999999999"),
					Email:       strPtr("ops@acme.test"),
				},
			},
			expectedStatus: http.StatusSeeOther,
			expectRedirect: true,
			expectCookie:   true,
		},
		{
			name: "missing phone - redirects to onboard",
			path: "/tracking",
			reader: &mockCompanySettingsReader{
				settings: domain.CompanySettings{
					CompanyName: "Acme Logistics",
					Address:     strPtr("123 Street"),
					Phone:       nil,
					Email:       strPtr("ops@acme.test"),
				},
			},
			expectedStatus: http.StatusSeeOther,
			expectRedirect: true,
			expectCookie:   true,
		},
		{
			name: "missing email - redirects to onboard",
			path: "/bookings",
			reader: &mockCompanySettingsReader{
				settings: domain.CompanySettings{
					CompanyName: "Acme Logistics",
					Address:     strPtr("123 Street"),
					Phone:       strPtr("+91 9999999999"),
					Email:       nil,
				},
			},
			expectedStatus: http.StatusSeeOther,
			expectRedirect: true,
			expectCookie:   true,
		},
		{
			name: "error fetching settings - redirects to onboard",
			path: "/invoices",
			reader: &mockCompanySettingsReader{
				err: errors.New("db failure"),
			},
			expectedStatus: http.StatusSeeOther,
			expectRedirect: true,
			expectCookie:   true,
		},
		{
			name: "complete company settings without GSTIN (Non-GST / RCM mode) - passes through",
			path: "/dashboard",
			reader: &mockCompanySettingsReader{
				settings: domain.CompanySettings{
					CompanyName: "Acme Logistics",
					Address:     strPtr("123 Street"),
					Phone:       strPtr("+91 9999999999"),
					Email:       strPtr("ops@acme.test"),
					GSTNumber:   nil, // Non-GST / exempt
				},
			},
			expectedStatus: http.StatusOK,
			expectRedirect: false,
			expectCookie:   false,
		},
		{
			name: "complete company settings with GSTIN - passes through",
			path: "/dashboard",
			reader: &mockCompanySettingsReader{
				settings: domain.CompanySettings{
					CompanyName: "Acme Logistics",
					Address:     strPtr("123 Street"),
					Phone:       strPtr("+91 9999999999"),
					Email:       strPtr("ops@acme.test"),
					GSTNumber:   strPtr("27ABCDE1234F1Z5"),
				},
			},
			expectedStatus: http.StatusOK,
			expectRedirect: false,
			expectCookie:   false,
		},
		{
			name: "exempt route /company/onboard with incomplete settings - passes through",
			path: "/company/onboard",
			reader: &mockCompanySettingsReader{
				settings: domain.CompanySettings{},
			},
			expectedStatus: http.StatusOK,
			expectRedirect: false,
			expectCookie:   false,
		},
		{
			name: "exempt route /logout - passes through",
			path: "/logout",
			reader: &mockCompanySettingsReader{
				settings: domain.CompanySettings{},
			},
			expectedStatus: http.StatusOK,
			expectRedirect: false,
			expectCookie:   false,
		},
		{
			name: "exempt route /static/css/main.css - passes through",
			path: "/static/css/main.css",
			reader: &mockCompanySettingsReader{
				settings: domain.CompanySettings{},
			},
			expectedStatus: http.StatusOK,
			expectRedirect: false,
			expectCookie:   false,
		},
		{
			name: "exempt route /login - passes through",
			path: "/login",
			reader: &mockCompanySettingsReader{
				settings: domain.CompanySettings{},
			},
			expectedStatus: http.StatusOK,
			expectRedirect: false,
			expectCookie:   false,
		},
		{
			name: "exempt route /pay/inv-123 - passes through",
			path: "/pay/inv-123",
			reader: &mockCompanySettingsReader{
				settings: domain.CompanySettings{},
			},
			expectedStatus: http.StatusOK,
			expectRedirect: false,
			expectCookie:   false,
		},
		{
			name: "exempt route /epod/trip-123 - passes through",
			path: "/epod/trip-123",
			reader: &mockCompanySettingsReader{
				settings: domain.CompanySettings{},
			},
			expectedStatus: http.StatusOK,
			expectRedirect: false,
			expectCookie:   false,
		},
		{
			name: "exempt route /share/token-123 - passes through",
			path: "/share/token-123",
			reader: &mockCompanySettingsReader{
				settings: domain.CompanySettings{},
			},
			expectedStatus: http.StatusOK,
			expectRedirect: false,
			expectCookie:   false,
		},
		{
			name:           "nil settings reader - passes through",
			path:           "/dashboard",
			reader:         nil,
			expectedStatus: http.StatusOK,
			expectRedirect: false,
			expectCookie:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mw := RequireCompanyCompliance(tc.reader)
			handler := mw(nextHandler)

			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			assert.Equal(t, tc.expectedStatus, rr.Code)
			if tc.expectRedirect {
				assert.Equal(t, "/company/onboard", rr.Header().Get("Location"))
			}
			if tc.expectCookie {
				var flashCookie *http.Cookie
				for _, c := range rr.Result().Cookies() {
					if c.Name == "flash_error" {
						flashCookie = c
						break
					}
				}
				require.NotNil(t, flashCookie, "expected flash_error cookie")
				assert.Contains(t, flashCookie.Value, "Please complete mandatory company compliance details to unlock fleet operations.")
			}
		})
	}
}
