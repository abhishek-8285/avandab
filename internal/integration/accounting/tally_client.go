package accounting

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

type tallyClient struct {
	cfg  Config
	http *http.Client
}

// tallyXML escapes user strings for the Tally XML envelope.
func tallyXML(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", `'`, "&apos;")
	return r.Replace(s)
}

func tallyEnvelope(inner string) string {
	return `<ENVELOPE><HEADER><TALLYREQUEST>Import Data</TALLYREQUEST></HEADER><BODY><IMPORTDATA><REQUESTDESC><REPORTNAME>Vouchers</REPORTNAME></REQUESTDESC><REQUESTDATA>` + inner + `</REQUESTDATA></IMPORTDATA></BODY></ENVELOPE>`
}

// tallyPost sends one XML envelope to the Tally server (default http://localhost:9000).
// Non-2xx surfaces tally_unavailable; a <LINEERROR> in the body is a rejected import.
func (c *tallyClient) tallyPost(ctx context.Context, body string) (string, error) {
	endpoint := c.cfg.Endpoint
	if endpoint == "" || endpoint == "https://api.accounting.example.com" {
		endpoint = "http://localhost:9000"
	}
	cl := c.http
	if cl == nil {
		cl = &http.Client{Timeout: 15 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("tally_unavailable: build request: %w", err)
	}
	req.Header.Set("Content-Type", "text/xml")
	resp, err := cl.Do(req)
	if err != nil {
		return "", fmt.Errorf("tally_unavailable: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("tally_unavailable: status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if strings.Contains(string(raw), "<LINEERROR>") {
		return "", fmt.Errorf("tally_rejected: %s", strings.TrimSpace(string(raw)))
	}
	return string(raw), nil
}

func (c *tallyClient) ExportInvoice(ctx context.Context, invoice ExportedInvoice) (ExportResult, error) {
	slog.Default().Info("[accounting:tally] ExportInvoice called", "endpoint", c.cfg.Endpoint, "enabled", c.cfg.Enabled, "invoice", invoice.InvoiceNumber)
	if !c.cfg.Enabled {
		return ExportResult{}, ErrDisabled
	}
	if !c.cfg.UseMock {
		var lines strings.Builder
		for _, li := range invoice.LineItems {
			fmt.Fprintf(&lines, `<LEDGERENTRIESLIST><LEDGERNAME>%s</LEDGERNAME><AMOUNT>%.2f</AMOUNT></LEDGERENTRIESLIST>`,
				tallyXML(li.Description), li.Amount)
		}
		body := tallyEnvelope(fmt.Sprintf(`<TALLYMESSAGE><VOUCHER VCHTYPE="Sales" ACTION="Create">`+
			`<VOUCHERNUMBER>%s</VOUCHERNUMBER><DATE>%s</DATE>`+
			`<PARTYNAME>%s</PARTYNAME><PARTYGSTIN>%s</PARTYGSTIN>`+
			`<LEDGERENTRIESLIST><LEDGERNAME>%s</LEDGERNAME><AMOUNT>%.2f</AMOUNT></LEDGERENTRIESLIST>%s</VOUCHER>`,
			tallyXML(invoice.InvoiceNumber), invoice.InvoiceDate.Format("20060102"),
			tallyXML(invoice.CustomerName), tallyXML(invoice.CustomerGSTIN),
			tallyXML(invoice.CustomerName), invoice.TotalAmount, lines.String()) + `</TALLYMESSAGE>`)
		if _, err := c.tallyPost(ctx, body); err != nil {
			return ExportResult{}, err
		}
		return ExportResult{
			SyncID:     uuid.New().String(),
			Status:     "SUCCESS",
			ExternalID: "TALLY-" + invoice.InvoiceNumber,
			Message:    "Invoice exported to Tally successfully",
		}, nil
	}
	return ExportResult{
		SyncID:     uuid.New().String(),
		Status:     "SUCCESS",
		ExternalID: "TALLY-MOCK-INV-" + invoice.InvoiceNumber,
		Message:    "Invoice exported to Tally successfully (mock)",
	}, nil
}

func (c *tallyClient) SyncContacts(ctx context.Context, contacts []Contact) (SyncResult, error) {
	slog.Default().Info("[accounting:tally] SyncContacts called", "endpoint", c.cfg.Endpoint, "enabled", c.cfg.Enabled, "count", len(contacts))
	if !c.cfg.Enabled {
		return SyncResult{}, ErrDisabled
	}
	if !c.cfg.UseMock {
		var errs []string
		synced := 0
		for _, ct := range contacts {
			body := tallyEnvelope(fmt.Sprintf(`<TALLYMESSAGE><LEDGER NAME="%s" ACTION="Create">`+
				`<ADDRESS>%s</ADDRESS><LEDGERPHONE>%s</LEDGERPHONE><LEDGEREMAIL>%s</LEDGEREMAIL>`+
				`<PARTYGSTIN>%s</PARTYGSTIN></LEDGER>`,
				tallyXML(ct.Name), tallyXML(ct.Address), tallyXML(ct.Phone),
				tallyXML(ct.Email), tallyXML(ct.GSTIN)) + `</TALLYMESSAGE>`)
			if _, err := c.tallyPost(ctx, body); err != nil {
				errs = append(errs, ct.Name+": "+err.Error())
				continue
			}
			synced++
		}
		return SyncResult{Synced: synced, Failed: len(errs), Errors: errs,
			Message: fmt.Sprintf("Synced %d/%d contacts to Tally", synced, len(contacts))}, nil
	}
	return SyncResult{
		Synced:  len(contacts),
		Failed:  0,
		Errors:  nil,
		Message: fmt.Sprintf("Synced %d contacts to Tally (mock)", len(contacts)),
	}, nil
}

func (c *tallyClient) PushJournalEntry(ctx context.Context, entry JournalEntry) (JournalEntryResult, error) {
	slog.Default().Info("[accounting:tally] PushJournalEntry called", "endpoint", c.cfg.Endpoint, "enabled", c.cfg.Enabled, "reference", entry.Reference)
	if !c.cfg.Enabled {
		return JournalEntryResult{}, ErrDisabled
	}
	if !c.cfg.UseMock {
		var lines strings.Builder
		for _, l := range entry.Lines {
			amt := l.Debit
			if amt == 0 {
				amt = -l.Credit // Tally convention: negative = credit
			}
			fmt.Fprintf(&lines, `<LEDGERENTRIESLIST><LEDGERNAME>%s</LEDGERNAME><AMOUNT>%.2f</AMOUNT></LEDGERENTRIESLIST>`,
				tallyXML(l.Account), amt)
		}
		body := tallyEnvelope(fmt.Sprintf(`<TALLYMESSAGE><VOUCHER VCHTYPE="Journal" ACTION="Create">`+
			`<VOUCHERNUMBER>%s</VOUCHERNUMBER><DATE>%s</DATE><NARRATION>%s</NARRATION>%s</VOUCHER>`,
			tallyXML(entry.Reference), entry.EntryDate.Format("20060102"),
			tallyXML(entry.Narration), lines.String()) + `</TALLYMESSAGE>`)
		if _, err := c.tallyPost(ctx, body); err != nil {
			return JournalEntryResult{}, err
		}
		return JournalEntryResult{
			EntryID: "TALLY-JE-" + entry.Reference,
			Status:  "SUCCESS",
			Message: "Journal entry pushed to Tally successfully",
		}, nil
	}
	return JournalEntryResult{
		EntryID: "TALLY-MOCK-JE-" + uuid.New().String()[:8],
		Status:  "SUCCESS",
		Message: "Journal entry pushed to Tally successfully (mock)",
	}, nil
}
