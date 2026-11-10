package reportbuilder

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebhookDelivery_PostsSignedPayload(t *testing.T) {
	var gotBody []byte
	var gotMethod, gotSig, gotEvent, gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotBody, _ = io.ReadAll(r.Body)
		gotSig = r.Header.Get("X-Report-Signature")
		gotEvent = r.Header.Get("X-Report-Event")
		gotCT = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	secret := "whsec-report"
	delivery := NewWebhookDelivery(srv.Client(), secret, nil)
	artifact := ReportArtifact{
		ReportID: "rep-1", ReportName: "Fleet", ExecutionID: "exec-1",
		Format: "CSV", Data: []byte("a,b\n1,2\n"), RowCount: 1,
	}
	sched := &ReportSchedule{ID: "sched-1", Name: "Nightly", Channel: "WEBHOOK", Recipients: []string{srv.URL}}
	require.NoError(t, delivery.Deliver(context.Background(), "tenant-1", sched, artifact))

	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, "application/json", gotCT)
	assert.Equal(t, "report.scheduled", gotEvent)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(gotBody)
	assert.Equal(t, "sha256="+hex.EncodeToString(mac.Sum(nil)), gotSig, "payload must be HMAC-SHA256 signed")

	var payload map[string]any
	require.NoError(t, json.Unmarshal(gotBody, &payload))
	assert.Equal(t, "report.scheduled", payload["event"])
	assert.Equal(t, "tenant-1", payload["tenant_id"])
	assert.Equal(t, "sched-1", payload["schedule_id"])
	assert.Equal(t, "CSV", payload["format"])
	assert.Equal(t, "text/csv", payload["content_type"])
	decoded, err := base64.StdEncoding.DecodeString(payload["data_base64"].(string))
	require.NoError(t, err)
	assert.Equal(t, artifact.Data, decoded)
}

func TestWebhookDelivery_FansOutToEveryEndpoint(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	delivery := NewWebhookDelivery(srv.Client(), "", nil)
	sched := &ReportSchedule{ID: "s", Channel: "WEBHOOK", Recipients: []string{srv.URL + "/a", srv.URL + "/b"}}
	require.NoError(t, delivery.Deliver(context.Background(), "t", sched, ReportArtifact{ReportName: "R", Data: []byte("x")}))
	assert.Equal(t, int32(2), atomic.LoadInt32(&hits))
}

func TestWebhookDelivery_NoSecretOmitsSignature(t *testing.T) {
	var gotSig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig = r.Header.Get("X-Report-Signature")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	delivery := NewWebhookDelivery(srv.Client(), "", nil)
	require.NoError(t, delivery.Deliver(context.Background(), "t",
		&ReportSchedule{ID: "s", Channel: "WEBHOOK", Recipients: []string{srv.URL}},
		ReportArtifact{ReportName: "R", Data: []byte("x")}))
	assert.Empty(t, gotSig)
}

func TestWebhookDelivery_RefusesInvalidInput(t *testing.T) {
	delivery := NewWebhookDelivery(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })).Client(), "", nil)
	artifact := ReportArtifact{ReportName: "R", Data: []byte("x")}

	err := delivery.Deliver(context.Background(), "t", &ReportSchedule{ID: "s", Channel: "EMAIL", Recipients: []string{"https://example.test"}}, artifact)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "channel")

	err = delivery.Deliver(context.Background(), "t", &ReportSchedule{ID: "s", Channel: "WEBHOOK"}, artifact)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no endpoint")

	err = delivery.Deliver(context.Background(), "t", &ReportSchedule{ID: "s", Channel: "WEBHOOK", Recipients: []string{"ftp://x"}}, artifact)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid endpoint")

	err = delivery.Deliver(context.Background(), "t", &ReportSchedule{ID: "s", Channel: "WEBHOOK", Recipients: []string{"https://example.test"}}, ReportArtifact{ReportName: "R"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestWebhookDelivery_Non2xxFailsHonestly(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hits, 1) == 1 {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("upstream exploded"))
	}))
	defer srv.Close()

	delivery := NewWebhookDelivery(srv.Client(), "", nil)
	err := delivery.Deliver(context.Background(), "t",
		&ReportSchedule{ID: "s", Channel: "WEBHOOK", Recipients: []string{srv.URL + "/ok", srv.URL + "/bad"}},
		ReportArtifact{ReportName: "R", Data: []byte("x")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "1/2 endpoints")
	assert.Contains(t, err.Error(), "HTTP 500")
	assert.Contains(t, err.Error(), "upstream exploded")
}

type stubDelivery struct {
	configured bool
	got        []string
}

func (s *stubDelivery) Configured() bool { return s.configured }
func (s *stubDelivery) Deliver(_ context.Context, _ string, sched *ReportSchedule, _ ReportArtifact) error {
	s.got = append(s.got, sched.Channel)
	return nil
}

func TestChannelRouter_ConfiguredForAndRoutes(t *testing.T) {
	email := &stubDelivery{configured: false}
	webhook := &stubDelivery{configured: true}
	router := NewChannelRouter(email, webhook)

	assert.False(t, router.ConfiguredFor("EMAIL"))
	assert.True(t, router.ConfiguredFor("WEBHOOK"))
	assert.True(t, router.Configured())
	require.NoError(t, router.Deliver(context.Background(), "t", &ReportSchedule{ID: "s", Channel: "WEBHOOK"}, ReportArtifact{Data: []byte("x")}))
	assert.Equal(t, []string{"WEBHOOK"}, webhook.got)
	assert.Empty(t, email.got)
	require.Error(t, router.Deliver(context.Background(), "t", &ReportSchedule{ID: "s", Channel: "EMAIL"}, ReportArtifact{Data: []byte("x")}))
}

func TestTriggerSchedule_WebhookChannelLifecycle(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	db := setupTestDB(t)
	defer db.Close()
	router := NewChannelRouter(NewEmailDelivery(&fakeRichSender{configured: false}, nil), NewWebhookDelivery(srv.Client(), "s3cret", nil))
	svc := NewServiceWithDelivery(NewRepository(db), router)

	sched, err := svc.CreateSchedule(context.Background(), "tenant-1", "u1", CreateScheduleInput{
		ReportID: "sys-rep-trip-profitability", Name: "Nightly webhook", Frequency: "DAILY",
		Format: "CSV", Channel: "WEBHOOK", Recipients: []string{srv.URL},
	})
	require.NoError(t, err)

	_, err = svc.TriggerSchedule(context.Background(), "tenant-1", sched.ID)
	require.NoError(t, err)

	after, err := svc.GetSchedule(context.Background(), "tenant-1", sched.ID)
	require.NoError(t, err)
	assert.Equal(t, "SUCCESS", after.LastStatus)
	assert.Equal(t, int32(1), atomic.LoadInt32(&hits))
}

func TestTriggerSchedule_WebhookUnconfiguredStaysPending(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	router := NewChannelRouter(NewEmailDelivery(&fakeRichSender{configured: false}, nil), nil)
	svc := NewServiceWithDelivery(NewRepository(db), router)

	sched, err := svc.CreateSchedule(context.Background(), "tenant-1", "u1", CreateScheduleInput{
		ReportID: "sys-rep-trip-profitability", Name: "No webhook", Frequency: "DAILY",
		Format: "CSV", Channel: "WEBHOOK", Recipients: []string{"https://example.test/hook"},
	})
	require.NoError(t, err)

	_, err = svc.TriggerSchedule(context.Background(), "tenant-1", sched.ID)
	require.Error(t, err, "unconfigured webhook channel must not report success")

	after, err := svc.GetSchedule(context.Background(), "tenant-1", sched.ID)
	require.NoError(t, err)
	assert.Equal(t, "PENDING_DELIVERY", after.LastStatus)
	assert.Contains(t, after.LastError, "delivery not configured")
}
