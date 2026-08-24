package test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/apiversion"
	"transport-app/internal/integration"
	"transport-app/internal/integration/ewaybill"
	"transport-app/internal/integration/fastag"
	"transport-app/internal/openapispec"
)

func TestAPIVersionsEndpoint(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/api/versions", apiversion.VersionsHandler)

	req := httptest.NewRequest(http.MethodGet, "/api/versions", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	assert.Equal(t, apiversion.V1, body["current"])
	versions, ok := body["versions"].([]interface{})
	require.True(t, ok)
	assert.Len(t, versions, 2)
}

func TestOpenAPISpecEndpoints(t *testing.T) {
	r := chi.NewRouter()
	openapispec.RegisterRoutes(r)

	t.Run("json", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		require.Equal(t, http.StatusOK, rr.Code)
		assert.Equal(t, "application/json; charset=utf-8", rr.Header().Get("Content-Type"))
		var doc map[string]interface{}
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &doc))
		assert.Equal(t, "3.0.3", doc["openapi"])
	})

	t.Run("yaml", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		require.Equal(t, http.StatusOK, rr.Code)
		assert.Equal(t, "application/yaml; charset=utf-8", rr.Header().Get("Content-Type"))
		assert.Contains(t, rr.Body.String(), "openapi: 3.0.3")
	})
}

func TestAPIV2Health(t *testing.T) {
	r := chi.NewRouter()
	apiversion.MountV2(r, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/health", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "true", rr.Header().Get("Deprecation"))
	assert.NotEmpty(t, rr.Header().Get("Sunset"))
}

func TestAPIV2AliasRequiresAuth(t *testing.T) {
	r := chi.NewRouter()
	authMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		})
	}
	apiversion.MountV2(r, authMiddleware, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/bookings", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	require.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestIntegrationEndpoints(t *testing.T) {
	r := chi.NewRouter()
	r.Use(authInjectMiddleware)

	cfg := integration.Config{
		EWayBill: ewaybill.Config{Enabled: true, UseMock: true},
		FASTag:   fastag.Config{Enabled: true, UseMock: true},
	}
	h := integration.NewHandler(cfg, &stubAuthSvc{})
	h.Register(r)

	t.Run("ewaybill generate returns stub bill in mock mode", func(t *testing.T) {
		body := map[string]interface{}{
			"document_number": "DOC/2026/0001",
			"from_gstin":      "27AABCU9603R1ZX",
			"to_gstin":        "07AAACP0000M1Z9",
			"transporter_id":  "27AABCU9603R1ZX",
			"vehicle_number":  "MH12AB1234",
		}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/ewaybill/generate", bytes.NewReader(b))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, nil))
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		require.Equal(t, http.StatusCreated, rr.Code, "body: %s", rr.Body.String())
		var res map[string]interface{}
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &res))
		assert.NotEmpty(t, res["ewb_number"])
	})

	t.Run("fastag balance returns stub balance in explicit mock mode", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/integrations/fastag/balance?vehicle_number=MH12AB1234&tag_id=TAG123", nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		require.Equal(t, http.StatusOK, rr.Code, "body: %s", rr.Body.String())
		var res map[string]interface{}
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &res))
		assert.Equal(t, 2475.50, res["balance"])
	})
}
