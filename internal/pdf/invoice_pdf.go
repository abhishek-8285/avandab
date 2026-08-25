package pdf

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
)

// page layout constants (A4 portrait, millimetres).
const (
	marginL    = 14
	marginR    = 14
	marginT    = 12
	pageW      = 210
	contentW   = pageW - marginL - marginR // 182
	footerH    = 18
	colGrey    = 243
	lineGrey   = 210
	darkText   = 40
	mutedText  = 110
	accentBlue = 25
	greenVal   = 22
	redVal     = 192
)

// GenerateInvoicePDF renders an adaptive invoice face: GST itemized
// columns when breakdown data exists, a simple layout otherwise, and
// e-invoice artifacts (IRN/QR/EWB) only when populated. Currency is
// rendered as "Rs." (core PDF fonts carry no ₹ glyph).
func GenerateInvoicePDF(d InvoicePDFData) ([]byte, error) {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(marginL, marginT, marginR)
	pdf.SetAutoPageBreak(true, footerH)
	pdf.AliasNbPages("{nb}")
	pdf.SetFooterFunc(func() {
		pdf.SetY(-14)
		pdf.SetFont("Arial", "I", 8)
		pdf.SetTextColor(mutedText, mutedText, mutedText)
		pdf.CellFormat(0, 6,
			fmt.Sprintf("Generated %s", time.Now().Format("02 Jan 2006 15:04")),
			"", 0, "L", false, 0, "")
		pdf.CellFormat(0, 6, fmt.Sprintf("Page %d of {nb}", pdf.PageNo()),
			"", 0, "R", false, 0, "")
	})
	basic := pdf.UnicodeTranslatorFromDescriptor("")
	tr := func(s string) string {
		return basic(s)
	}

	pdf.AddPage()

	renderHeader(pdf, tr, d)
	renderParties(pdf, tr, d)
	if len(d.Items) > 0 && d.GSTBreakdown {
		renderItemsGST(pdf, tr, d)
	} else if len(d.Items) > 0 {
		renderItemsSimple(pdf, tr, d)
	}
	renderSummary(pdf, tr, d)
	if len(d.Payments) > 0 {
		renderPayments(pdf, tr, d)
	}
	renderEInvoice(pdf, tr, d)

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("pdf output: %w", err)
	}
	return buf.Bytes(), nil
}

// rowGuard starts a new page when a row of the given height would cross
// the footer zone — keeps table rows unsplit.
func rowGuard(pdf *fpdf.Fpdf, h float64) {
	_, pageH := pdf.GetPageSize()
	_, _, _, mb := pdf.GetMargins()
	if pdf.GetY()+h > pageH-mb-footerH+6 {
		pdf.AddPage()
	}
}

func renderHeader(pdf *fpdf.Fpdf, tr func(string) string, d InvoicePDFData) {
	// Seller block, left.
	pdf.SetFont("Arial", "B", 16)
	pdf.SetTextColor(accentBlue, 51, 128)
	pdf.CellFormat(105, 8, tr(d.Company.Name), "", 1, "L", false, 0, "")

	pdf.SetFont("Arial", "", 9)
	pdf.SetTextColor(darkText, darkText, darkText)
	for _, line := range []string{d.Company.Address, phoneEmailLine(d.Company)} {
		if line == "" {
			continue
		}
		pdf.MultiCell(105, 4.2, tr(line), "", "L", false)
	}
	if d.Company.GSTIN != "" {
		pdf.SetFont("Arial", "B", 9)
		pdf.CellFormat(105, 5, tr("GSTIN: "+d.Company.GSTIN+
			stateSuffix(d.Company.StateCode)), "", 1, "L", false, 0, "")
	}
	sellerY := pdf.GetY()

	// Title + core metadata, right column.
	titleX := 112.0
	pdf.SetXY(titleX, marginT)
	pdf.SetFont("Arial", "B", 20)
	pdf.SetTextColor(darkText, darkText, darkText)
	pdf.CellFormat(84, 10, "TAX INVOICE", "", 2, "R", false, 0, "")

	pdf.SetFont("Arial", "", 9)
	meta := [][2]string{
		{"Invoice No:", d.InvoiceNumber},
		{"Invoice Date:", d.InvoiceDate},
	}
	if d.DueDate != "" {
		meta = append(meta, [2]string{"Due Date:", d.DueDate})
	}
	if d.PaymentStatus != "" {
		meta = append(meta, [2]string{"Payment Status:", d.PaymentStatus})
	}
	for _, m := range meta {
		pdf.SetX(titleX)
		pdf.CellFormat(30, 5, m[0], "", 0, "L", false, 0, "")
		pdf.CellFormat(54, 5, tr(m[1]), "", 1, "R", false, 0, "")
	}

	// Divider below whichever column is taller.
	y := sellerY
	if pdf.GetY() > y {
		y = pdf.GetY()
	}
	pdf.SetY(y + 2)
	pdf.SetDrawColor(lineGrey, lineGrey, lineGrey)
	pdf.SetLineWidth(0.4)
	pdf.Line(marginL, pdf.GetY(), pageW-marginR, pdf.GetY())
	pdf.Ln(4)
}

func renderParties(pdf *fpdf.Fpdf, tr func(string) string, d InvoicePDFData) {
	half := contentW / 2.0

	boxLabel(pdf, tr("Bill To"), half)
	pdf.SetFont("Arial", "B", 10)
	name := d.Customer.Name
	if d.Customer.Name == "" {
		name = "(customer on file)"
	}
	pdf.MultiCell(half, 4.6, tr(name), "", "L", false)
	pdf.SetFont("Arial", "", 9)
	pdf.SetTextColor(darkText, darkText, darkText)
	if d.Customer.Address != "" {
		pdf.MultiCell(half, 4.2, tr(d.Customer.Address), "", "L", false)
	}
	if d.Customer.GSTIN != "" {
		pdf.SetFont("Arial", "B", 9)
		pdf.CellFormat(half, 5, tr("GSTIN: "+d.Customer.GSTIN+
			stateSuffix(d.Customer.StateCode)), "", 1, "L", false, 0, "")
	} else {
		pdf.Ln(1)
	}

	// Right column: place of supply + reverse charge.
	rightX := marginL + half
	partyRight := [][2]string{
		{"Place of Supply:", d.PlaceOfSupply},
		{"Reverse Charge:", yesNo(d.ReverseCharge)},
	}
	for _, kv := range partyRight {
		pdf.SetX(rightX)
		pdf.CellFormat(38, 5, kv[0], "", 0, "L", false, 0, "")
		pdf.CellFormat(half-38, 5, tr(kv[1]), "", 1, "R", false, 0, "")
	}
	pdf.Ln(4)
}

func boxLabel(pdf *fpdf.Fpdf, label string, w float64) {
	pdf.SetFont("Arial", "B", 8.5)
	pdf.SetTextColor(mutedText, mutedText, mutedText)
	pdf.CellFormat(w, 5, label, "", 1, "L", false, 0, "")
}

func renderItemsGST(pdf *fpdf.Fpdf, tr func(string) string, d InvoicePDFData) {
	intra := d.IntraState
	cols := []float64{6, 44, 16, 13, 17, 19, 14, 14, 39}
	headers := []string{"#", "Description", "HSN/SAC", "Qty", "Rate",
		"Taxable", "CGST", "SGST", "Amount"}
	if !intra {
		cols = []float64{6, 44, 16, 13, 17, 19, 24, 43}
		headers = []string{"#", "Description", "HSN/SAC", "Qty", "Rate",
			"Taxable", "IGST", "Amount"}
	}

	tableHeader(pdf, headers, cols)

	pdf.SetFont("Arial", "", 8.5)
	pdf.SetTextColor(darkText, darkText, darkText)
	for i, it := range d.Items {
		rowGuard(pdf, 7)
		cells := []struct {
			w     float64
			text  string
			align string
		}{
			{cols[0], fmt.Sprint(i + 1), "C"},
			{cols[1], tr(clip(it.Description, 60)), "L"},
			{cols[2], it.HSNSAC, "C"},
			{cols[3], it.Quantity, "C"},
		}
		for _, c := range cells {
			pdf.CellFormat(c.w, 7, c.text, "LR", 0, c.align, false, 0, "")
		}
		cellFit(pdf, cols[4], 7, 8.5, rs(it.Rate), "LR")
		cellFit(pdf, cols[5], 7, 8.5, rs(it.TaxableValue), "LR")
		if intra {
			cellFit(pdf, cols[6], 7, 8.5, rs(it.CGST), "LR")
			cellFit(pdf, cols[7], 7, 8.5, rs(it.SGST), "LR")
		} else {
			cellFit(pdf, cols[6], 7, 8.5, rs(it.IGST), "LR")
		}
		cellFit(pdf, cols[len(cols)-1], 7, 8.5, rs(it.Total), "LR")
		pdf.Ln(7)
	}
	tableBottom(pdf, cols)
}

func renderItemsSimple(pdf *fpdf.Fpdf, tr func(string) string, d InvoicePDFData) {
	cols := []float64{8, 108, 22, 22, 22}
	tableHeader(pdf, []string{"#", "Description", "Qty", "Rate (Rs.)", "Amount"}, cols)

	pdf.SetFont("Arial", "", 9)
	pdf.SetTextColor(darkText, darkText, darkText)
	for i, it := range d.Items {
		rowGuard(pdf, 7)
		pdf.CellFormat(cols[0], 7, fmt.Sprint(i+1), "LR", 0, "C", false, 0, "")
		pdf.CellFormat(cols[1], 7, tr(clip(it.Description, 90)), "LR", 0, "L", false, 0, "")
		pdf.CellFormat(cols[2], 7, it.Quantity, "LR", 0, "C", false, 0, "")
		cellFit(pdf, cols[3], 7, 9, rs(it.Rate), "LR")
		cellFit(pdf, cols[4], 7, 9, rs(it.Total), "LR")
		pdf.Ln(7)
	}
	tableBottom(pdf, cols)
}

func tableHeader(pdf *fpdf.Fpdf, headers []string, cols []float64) {
	rowGuard(pdf, 8)
	pdf.SetFont("Arial", "B", 8.5)
	pdf.SetFillColor(colGrey, colGrey, colGrey)
	pdf.SetTextColor(darkText, darkText, darkText)
	aligns := map[int]string{1: "L", len(headers) - 1: "R"}
	for i, h := range headers {
		a := "C"
		if len(headers) <= 5 && i == 1 {
			a = "L"
		}
		if v, ok := aligns[i]; ok && len(headers) > 5 {
			a = v
		}
		pdf.CellFormat(cols[i], 7.5, h, "1", 0, a, true, 0, "")
	}
	pdf.Ln(-1)
}

func tableBottom(pdf *fpdf.Fpdf, cols []float64) {
	pdf.SetDrawColor(lineGrey, lineGrey, lineGrey)
	w := 0.0
	for _, c := range cols[:len(cols)-1] {
		w += c
	}
	pdf.CellFormat(w, 0.2, "", "T", 0, "L", false, 0, "")
	pdf.CellFormat(cols[len(cols)-1], 0.2, "", "T", 1, "L", false, 0, "")
	pdf.Ln(2)
}

func renderSummary(pdf *fpdf.Fpdf, tr func(string) string, d InvoicePDFData) {
	labelW, valW := 118.0, 32.0

	sumRow := func(label string, val float64, bold, fill bool, color int) {
		pdf.SetFont("Arial", b(bold), 9.5)
		pdf.SetTextColor(color, color, color)
		pdf.CellFormat(labelW, 6.4, label, "", 0, "R", fill, 0, "")
		pdf.CellFormat(valW, 6.4, rs(val), "", 1, "R", fill, 0, "")
	}

	if d.Subtotal > 0 || d.Discount > 0 || !d.GSTBreakdown {
		sumRow("Subtotal", d.Subtotal, false, false, darkText)
		if d.Discount > 0 {
			sumRow("Discount", -d.Discount, false, false, darkText)
		}
	}
	if d.GSTBreakdown {
		pdf.SetFont("Arial", "", 9.5)
		pdf.SetTextColor(darkText, darkText, darkText)
		pdf.CellFormat(labelW, 6.4, "Taxable Value", "", 0, "R", false, 0, "")
		pdf.CellFormat(valW, 6.4, rs(d.TaxableTotal), "", 1, "R", false, 0, "")
		if d.IntraState {
			sumRow("CGST", d.CGST, false, false, darkText)
			sumRow("SGST", d.SGST, false, false, darkText)
		} else {
			sumRow("IGST", d.IGST, false, false, darkText)
		}
	} else if d.Tax > 0 {
		sumRow("Tax", d.Tax, false, false, darkText)
	}
	if d.RoundOff != 0 {
		sumRow("Round Off", d.RoundOff, false, false, darkText)
	}

	sumRow("Total", d.Total, true, true, darkText)
	sumRow("Paid", d.Paid, false, false, greenVal)
	color := greenVal
	if d.Balance > 0.004 {
		color = redVal
	}
	sumRow("Balance Due", d.Balance, true, true, color)

	// Amount in words under the summary.
	pdf.Ln(2)
	pdf.SetFont("Arial", "B", 8.5)
	pdf.SetTextColor(mutedText, mutedText, mutedText)
	pdf.CellFormat(contentW, 4.6, tr("Amount in Words: "+AmountInWordsIndian(d.Total)),
		"", 1, "L", false, 0, "")
	pdf.Ln(2)
}

func renderPayments(pdf *fpdf.Fpdf, tr func(string) string, d InvoicePDFData) {
	rowGuard(pdf, 10+float64(len(d.Payments))*6)
	boxLabel(pdf, tr("Payments Received"), contentW)
	cols := []float64{30, 34, 78, 40}
	pdf.SetFont("Arial", "B", 8.5)
	head := []string{"Date", "Method", "Reference", "Amount (Rs.)"}
	for i, hd := range head {
		a := "L"
		if i >= 2 {
			a = "R"
		}
		pdf.CellFormat(cols[i], 6, hd, "1", 0, a, false, 0, "")
	}
	pdf.Ln(-1)
	pdf.SetFont("Arial", "", 8.5)
	pdf.SetTextColor(darkText, darkText, darkText)
	for _, p := range d.Payments {
		rowGuard(pdf, 6)
		pdf.CellFormat(cols[0], 6, p.Date, "1", 0, "L", false, 0, "")
		pdf.CellFormat(cols[1], 6, p.Method, "1", 0, "L", false, 0, "")
		pdf.CellFormat(cols[2], 6, tr(p.Ref), "1", 0, "R", false, 0, "")
		pdf.CellFormat(cols[3], 6, rs(p.Amount), "1", 1, "R", false, 0, "")
	}
	pdf.Ln(2)
}

func renderEInvoice(pdf *fpdf.Fpdf, tr func(string) string, d InvoicePDFData) {
	hasIRN := d.IRN != ""
	hasEWB := d.EWBNumber != ""
	if !hasIRN && !hasEWB && len(d.SignedQR) == 0 {
		return
	}
	rowGuard(pdf, 42)

	boxLabel(pdf, tr("E-Invoice / E-Way Bill"), contentW)

	qrSize := 0.0
	if len(d.SignedQR) > 4 {
		if isPNG(d.SignedQR) || isJPEG(d.SignedQR) {
			var opt fpdf.ImageOptions
			opt.ImageType = imgType(d.SignedQR)
			name := "signedqr-" + d.InvoiceNumber
			pdf.RegisterImageOptionsReader(name, opt, bytes.NewReader(d.SignedQR))
			pdf.ImageOptions(name, marginL, pdf.GetY(), 26, 0, false, opt, 0, "")
			qrSize = 26
		}
	}

	fields := [][2]string{}
	if hasIRN {
		fields = append(fields, [2]string{"IRN:", d.IRN})
		if d.IRNAckNo != "" {
			fields = append(fields, [2]string{"Ack No:", d.IRNAckNo})
		}
		if d.IRNAckDate != "" {
			fields = append(fields, [2]string{"Ack Date:", d.IRNAckDate})
		}
	}
	if hasEWB {
		fields = append(fields, [2]string{"E-Way Bill No:", d.EWBNumber})
	}

	textX := float64(marginL)
	if qrSize > 0 {
		textX += qrSize + 4
	}
	pdf.SetXY(textX, pdf.GetY())
	pdf.SetFont("Arial", "", 8)
	pdf.SetTextColor(darkText, darkText, darkText)
	startY := pdf.GetY()
	for i, kv := range fields {
		pdf.SetX(textX)
		style := "B"
		if i > 0 {
			style = ""
		}
		pdf.SetFont("Arial", style, 8)
		pdf.CellFormat(26, 4.6, kv[0], "", 0, "L", false, 0, "")
		pdf.SetFont("Arial", "", 8)
		pdf.MultiCell(contentW-(textX-marginL)-26, 4.6, tr(clip(kv[1], 120)), "", "L", false)
		if qrSize == 0 {
			continue
		}
	}
	if qrSize > 0 && pdf.GetY() < startY+qrSize {
		pdf.SetY(startY + qrSize)
	}
	pdf.Ln(2)
}

// ── helpers ──────────────────────────────────────────────────────────

func b(bold bool) string {
	if bold {
		return "B"
	}
	return ""
}

func stateSuffix(code string) string {
	if code == "" {
		return ""
	}
	return " (" + code + ")"
}

func yesNo(v bool) string {
	if v {
		return "Yes"
	}
	return "No"
}

func phoneEmailLine(p PDFParty) string {
	out := ""
	if p.Phone != "" {
		out += "Ph: " + p.Phone
	}
	if p.Email != "" {
		if out != "" {
			out += "  |  "
		}
		out += p.Email
	}
	return out
}

func clip(s string, maxChars int) string {
	r := []rune(s)
	if len(r) <= maxChars {
		return s
	}
	return string(r[:maxChars-1]) + "…"
}

// cellFit writes a right-aligned currency string into a cell of width w,
// shrinking the font size down to 7pt if it would otherwise overflow,
// then restores the caller's font size. Prevents "Rs. 2,000.00" from
// bleeding into adjacent columns.
func cellFit(pdf *fpdf.Fpdf, w, h, baseSize float64, text, border string) {
	restore := func() { pdf.SetFont("Arial", "", baseSize) }
	for _, sz := range []float64{baseSize, 8.5, 8, 7.5, 7} {
		pdf.SetFont("Arial", "", sz)
		if pdf.GetStringWidth(text)+1.0 <= w {
			pdf.CellFormat(w, h, text, border, 0, "R", false, 0, "")
			restore()
			return
		}
	}
	pdf.SetFont("Arial", "", 7)
	pdf.CellFormat(w, h, text, border, 0, "R", false, 0, "")
	restore()
}

// rs formats rupee amounts with thousand separators (Indian grouping).
func rs(v float64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	intPart := int64(v)
	frac := int((v-float64(intPart))*100 + 0.5)
	if frac >= 100 {
		intPart++
		frac = 0
	}
	s := fmt.Sprintf("%d", intPart)
	n := len(s)
	var grouped string
	switch {
	case n <= 3:
		grouped = s
	default:
		last3 := s[n-3:]
		rest := s[:n-3]
		var groups []string
		for len(rest) > 2 {
			groups = append([]string{rest[len(rest)-2:]}, groups...)
			rest = rest[:len(rest)-2]
		}
		if rest != "" {
			groups = append([]string{rest}, groups...)
		}
		grouped = strings.Join(groups, ",") + "," + last3
	}
	sign := ""
	if neg {
		sign = "-"
	}
	return fmt.Sprintf("%sRs. %s.%02d", sign, grouped, frac)
}

func isPNG(b []byte) bool {
	return len(b) > 8 && b[0] == 0x89 && b[1] == 'P' && b[2] == 'N' && b[3] == 'G'
}

func isJPEG(b []byte) bool {
	return len(b) > 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF
}

func imgType(b []byte) string {
	if isPNG(b) {
		return "png"
	}
	if isJPEG(b) {
		return "jpg"
	}
	return ""
}
