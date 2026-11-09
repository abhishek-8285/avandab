package reportbuilder

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/operations/notifications"
)

type fakeSentEmail struct {
	to          string
	subject     string
	attachments []notifications.Attachment
}

type fakeRichSender struct {
	configured bool
	err        error
	sent       []fakeSentEmail
}

func (f *fakeRichSender) Send(context.Context, string, string, string) error         { return f.err }
func (f *fakeRichSender) Configured() bool                                           { return f.configured }
func (f *fakeRichSender) SendRich(context.Context, notifications.EmailMessage) error { return f.err }
func (f *fakeRichSender) SendHTML(context.Context, string, string, string, string) error {
	return f.err
}
func (f *fakeRichSender) SendWithAttachments(_ context.Context, to, subject, _, _ string, atts []notifications.Attachment) error {
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, fakeSentEmail{to: to, subject: subject, attachments: atts})
	return nil
}

func TestEmailDelivery_Configured(t *testing.T) {
	assert.False(t, NewEmailDelivery(&fakeRichSender{configured: false}, nil).Configured())
	assert.True(t, NewEmailDelivery(&fakeRichSender{configured: true}, nil).Configured())
	assert.False(t, NewEmailDelivery(nil, nil).Configured())
}

func TestEmailDelivery_DeliversCSVToEveryRecipient(t *testing.T) {
	sender := &fakeRichSender{configured: true}
	delivery := NewEmailDelivery(sender, nil)
	sched := &ReportSchedule{
		ID: "sched-1", Name: "Monthly Fleet Report", Channel: "EMAIL", Format: "CSV",
		Recipients: []string{"ops@client.test", " accounts@client.test "},
	}
	artifact := ReportArtifact{ReportName: "Fleet Utilization", Format: "CSV", Data: []byte("a,b\n1,2\n"), RowCount: 1}

	require.NoError(t, delivery.Deliver(context.Background(), "tenant-1", sched, artifact))
	require.Len(t, sender.sent, 2)
	assert.Equal(t, "ops@client.test", sender.sent[0].to)
	assert.Equal(t, "accounts@client.test", sender.sent[1].to)
	assert.Contains(t, sender.sent[0].subject, "Monthly Fleet Report")
	require.Len(t, sender.sent[0].attachments, 1)
	att := sender.sent[0].attachments[0]
	assert.Contains(t, att.Filename, "Fleet-Utilization-")
	assert.Contains(t, att.Filename, ".csv")
	assert.Equal(t, "text/csv", att.ContentType)
	assert.Equal(t, artifact.Data, att.Data)
}

func TestEmailDelivery_JSONFormat(t *testing.T) {
	sender := &fakeRichSender{configured: true}
	delivery := NewEmailDelivery(sender, nil)
	require.NoError(t, delivery.Deliver(context.Background(), "tenant-1",
		&ReportSchedule{ID: "s", Name: "JSON", Channel: "EMAIL", Format: "JSON", Recipients: []string{"a@b.test"}},
		ReportArtifact{ReportName: "Data", Format: "JSON", Data: []byte("{}")}))
	att := sender.sent[0].attachments[0]
	assert.Contains(t, att.Filename, ".json")
	assert.Equal(t, "application/json", att.ContentType)
}

func TestEmailDelivery_RefusesUnsupportedChannelAndEmptyRecipients(t *testing.T) {
	delivery := NewEmailDelivery(&fakeRichSender{configured: true}, nil)
	artifact := ReportArtifact{ReportName: "R", Data: []byte("x")}

	err := delivery.Deliver(context.Background(), "t", &ReportSchedule{ID: "s", Channel: "WEBHOOK", Recipients: []string{"a@b.test"}}, artifact)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "channel")

	err = delivery.Deliver(context.Background(), "t", &ReportSchedule{ID: "s", Channel: "EMAIL"}, artifact)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no recipients")

	err = delivery.Deliver(context.Background(), "t", &ReportSchedule{ID: "s", Channel: "EMAIL", Recipients: []string{"not-an-email"}}, artifact)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid recipient")

	err = delivery.Deliver(context.Background(), "t", &ReportSchedule{ID: "s", Channel: "EMAIL", Recipients: []string{"a@b.test"}}, ReportArtifact{ReportName: "R"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestEmailDelivery_SendErrorFailsHonestly(t *testing.T) {
	delivery := NewEmailDelivery(&fakeRichSender{configured: true, err: errors.New("smtp down")}, nil)
	err := delivery.Deliver(context.Background(), "t",
		&ReportSchedule{ID: "s", Channel: "EMAIL", Recipients: []string{"a@b.test", "c@d.test"}},
		ReportArtifact{ReportName: "R", Data: []byte("x")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "2/2 recipients")
	assert.Contains(t, err.Error(), "smtp down")
}

func TestTriggerSchedule_EmailDeliveryLifecycle(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	sender := &fakeRichSender{configured: true}
	svc := NewServiceWithDelivery(NewRepository(db), NewEmailDelivery(sender, nil))
	ctx := context.Background()

	sched, err := svc.CreateSchedule(ctx, "tenant-1", "u1", CreateScheduleInput{
		ReportID:   "sys-rep-trip-profitability",
		Name:       "Daily profitability email",
		Frequency:  "DAILY",
		Format:     "CSV",
		Channel:    "EMAIL",
		Recipients: []string{"ops@fleet.local", "cfo@fleet.local"},
	})
	require.NoError(t, err)

	_, err = svc.TriggerSchedule(ctx, "tenant-1", sched.ID)
	require.NoError(t, err)

	after, err := svc.GetSchedule(ctx, "tenant-1", sched.ID)
	require.NoError(t, err)
	assert.Equal(t, "SUCCESS", after.LastStatus)
	assert.Empty(t, after.LastError)

	require.Len(t, sender.sent, 2, "one email per recipient")
	assert.Contains(t, sender.sent[0].attachments[0].Filename, ".csv")
	assert.NotEmpty(t, sender.sent[0].attachments[0].Data)
}

func TestTriggerSchedule_EmailDeliveryUnconfiguredStaysPending(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := NewServiceWithDelivery(NewRepository(db), NewEmailDelivery(&fakeRichSender{configured: false}, nil))
	ctx := context.Background()
	sched, err := svc.CreateSchedule(ctx, "tenant-1", "u1", CreateScheduleInput{
		ReportID: "sys-rep-trip-profitability", Name: "Daily", Frequency: "DAILY",
		Format: "CSV", Channel: "EMAIL", Recipients: []string{"ops@fleet.local"},
	})
	require.NoError(t, err)

	_, err = svc.TriggerSchedule(ctx, "tenant-1", sched.ID)
	require.Error(t, err, "unconfigured email must not report success")

	after, err := svc.GetSchedule(ctx, "tenant-1", sched.ID)
	require.NoError(t, err)
	assert.Equal(t, "PENDING_DELIVERY", after.LastStatus)
	assert.Contains(t, after.LastError, "delivery not configured")
}

func TestTriggerSchedule_EmailDeliveryFailureMarksFailed(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := NewServiceWithDelivery(NewRepository(db), NewEmailDelivery(&fakeRichSender{configured: true, err: errors.New("smtp down")}, nil))
	ctx := context.Background()
	sched, err := svc.CreateSchedule(ctx, "tenant-1", "u1", CreateScheduleInput{
		ReportID: "sys-rep-trip-profitability", Name: "Daily", Frequency: "DAILY",
		Format: "CSV", Channel: "EMAIL", Recipients: []string{"ops@fleet.local"},
	})
	require.NoError(t, err)

	_, err = svc.TriggerSchedule(ctx, "tenant-1", sched.ID)
	require.Error(t, err)

	after, err := svc.GetSchedule(ctx, "tenant-1", sched.ID)
	require.NoError(t, err)
	assert.Equal(t, "FAILED", after.LastStatus)
	assert.Contains(t, after.LastError, "smtp down")
}
