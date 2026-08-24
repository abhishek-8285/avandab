package pdf

// InvoicePDFData is the complete render input for GenerateInvoicePDF.
// The handler composes it from company_settings, customers,
// invoice_line_items and payments; the generator renders whatever is
// present and omits sections whose data is absent (adaptive layout).
type InvoicePDFData struct {
	Company  PDFParty
	Customer PDFParty

	InvoiceNumber string
	Status        string
	PaymentStatus string
	InvoiceDate   string
	DueDate       string
	PlaceOfSupply string
	ReverseCharge bool

	Subtotal float64
	Tax      float64
	Discount float64
	Total    float64
	Paid     float64
	Balance  float64

	// GST split (from invoices.cgst/sgst/igst or line-item sums).
	CGST float64
	SGST float64
	IGST float64
	// GSTBreakdown switches the items table into HSN/rate columns when
	// true (line items carry per-line tax data). Intra-state shows
	// CGST+SGST columns; inter-state shows IGST.
	GSTBreakdown bool
	IntraState   bool
	Items        []PDFLineItem
	TaxableTotal float64
	RoundOff     float64

	Payments []PDFPaymentRow

	IRN        string
	IRNAckNo   string
	IRNAckDate string
	SignedQR   []byte // raw image bytes (PNG/JPEG) only; URLs are never fetched
	EWBNumber  string
}

// PDFParty is a seller or buyer block on the invoice face.
type PDFParty struct {
	Name      string
	Address   string
	GSTIN     string
	StateCode string
	Phone     string
	Email     string
}

// PDFLineItem is one itemized row. Tax fields are populated only in the
// GST-breakdown layout.
type PDFLineItem struct {
	Description  string
	HSNSAC       string
	Quantity     string // pre-formatted, unit included ("1 NOS")
	Rate         float64
	TaxableValue float64
	CGST         float64
	SGST         float64
	IGST         float64
	Total        float64
}

// PDFPaymentRow is one entry in the payments received section.
type PDFPaymentRow struct {
	Date   string
	Method string
	Ref    string
	Amount float64
}
