package razorpay

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	rzp "github.com/razorpay/razorpay-go"
)

// ErrNotConfigured is returned when Razorpay credentials are absent.
var ErrNotConfigured = errors.New("razorpay credentials not configured")

// orderAPI is the subset of the Razorpay SDK used to create orders. It is an
// interface so unit tests can fake the network call.
type orderAPI interface {
	Create(data map[string]interface{}, extraHeaders map[string]string) (map[string]interface{}, error)
}

// resourceFetcher is the subset of the Razorpay SDK used to read an order or
// payment back from the gateway. It is an interface so the verify-binding
// checks are unit-testable without network access.
type resourceFetcher interface {
	Fetch(id string, queryParams map[string]interface{}, extraHeaders map[string]string) (map[string]interface{}, error)
}

// OrderCreator creates a Razorpay order for checkout. Satisfied by
// *RazorpayClient; faked in unit tests (Spec 11 §5.1).
type OrderCreator interface {
	CreateOrder(invoiceID string, amountINR float64, currency string) (*Order, error)
}

// SignatureVerifier validates the HMAC signature returned by Razorpay
// Checkout. Satisfied by *RazorpayClient; faked in unit tests.
type SignatureVerifier interface {
	VerifyPaymentSignature(orderID, paymentID, signature string) bool
}

// PaymentGateway reads gateway state during checkout verification.
//
// The checkout signature only proves "Razorpay signed order|payment". It says
// nothing about which invoice the money is for, nor how much was captured.
// Verification MUST therefore fetch both entities and check:
//   - payment.order_id == the order supplied by the caller
//   - payment.status == captured
//   - order.notes.invoice_id (or receipt) == the invoice being credited
//   - payment.amount == order.amount
//
// Without those checks a valid signature from any checkout can be replayed
// against another invoice.
type PaymentGateway interface {
	FetchPayment(paymentID string) (*PaymentDetails, error)
	FetchOrder(orderID string) (*OrderDetails, error)
}

// PaymentDetails is the server-trusted view of a Razorpay payment.
type PaymentDetails struct {
	ID       string
	OrderID  string
	Status   string
	Currency string
	Amount   int64 // paise
}

// Captured reports whether the payment has been captured.
func (p *PaymentDetails) Captured() bool { return p != nil && p.Status == "captured" }

// OrderDetails is the server-trusted view of a Razorpay order.
type OrderDetails struct {
	ID       string
	Status   string
	Receipt  string
	Currency string
	Amount   int64 // paise
	Notes    map[string]string
}

// Client is the Razorpay payment-gateway gateway, and implements OrderCreator,
// SignatureVerifier and PaymentGateway.
var (
	_ OrderCreator      = (*RazorpayClient)(nil)
	_ SignatureVerifier = (*RazorpayClient)(nil)
	_ PaymentGateway    = (*RazorpayClient)(nil)
)

type RazorpayClient struct {
	orderAPI   orderAPI
	orderFetch resourceFetcher
	payFetch   resourceFetcher
	keyID      string
	keySecret  string
}

type Order struct {
	ID       string `json:"id"`
	Entity   string `json:"entity"`
	Amount   int64  `json:"amount"` // in paise
	Currency string `json:"currency"`
	Receipt  string `json:"receipt"`
	Status   string `json:"status"`
}

func NewRazorpayClient(keyID, keySecret string) *RazorpayClient {
	c := &RazorpayClient{
		keyID:     keyID,
		keySecret: keySecret,
	}
	if keyID != "" && keySecret != "" {
		client := rzp.NewClient(keyID, keySecret)
		c.orderAPI = client.Order
		c.orderFetch = client.Order
		c.payFetch = client.Payment
	}
	return c
}

// CreateOrder generates a Razorpay Order ID for checkout.
func (c *RazorpayClient) CreateOrder(invoiceID string, amountINR float64, currency string) (*Order, error) {
	if c.orderAPI == nil {
		return nil, ErrNotConfigured
	}

	amountInPaise := int64(amountINR * 100)
	if currency == "" {
		currency = "INR"
	}

	data := map[string]interface{}{
		"amount":   amountInPaise,
		"currency": currency,
		"receipt":  fmt.Sprintf("rcpt_%s", invoiceID),
		"notes": map[string]interface{}{
			"invoice_id": invoiceID,
		},
	}

	body, err := c.orderAPI.Create(data, nil)
	if err != nil {
		return nil, err
	}

	orderID, _ := body["id"].(string)
	status, _ := body["status"].(string)

	return &Order{
		ID:       orderID,
		Amount:   amountInPaise,
		Currency: currency,
		Receipt:  fmt.Sprintf("rcpt_%s", invoiceID),
		Status:   status,
	}, nil
}

// VerifyPaymentSignature validates the cryptographic signature returned by
// Razorpay Checkout.
func (c *RazorpayClient) VerifyPaymentSignature(orderID, paymentID, signature string) bool {
	if c.keySecret == "" {
		return false
	}

	data := orderID + "|" + paymentID
	h := hmac.New(sha256.New, []byte(c.keySecret))
	h.Write([]byte(data))
	expectedSignature := h.Sum(nil)
	sigBytes, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}

	return hmac.Equal(expectedSignature, sigBytes)
}

// FetchPayment reads a payment from Razorpay. The caller must treat the
// returned values as authoritative (status, amount, order binding).
func (c *RazorpayClient) FetchPayment(paymentID string) (*PaymentDetails, error) {
	if c == nil || c.payFetch == nil {
		return nil, ErrNotConfigured
	}
	body, err := c.payFetch.Fetch(paymentID, nil, nil)
	if err != nil {
		return nil, err
	}
	return &PaymentDetails{
		ID:       stringField(body, "id"),
		OrderID:  stringField(body, "order_id"),
		Status:   stringField(body, "status"),
		Currency: stringField(body, "currency"),
		Amount:   int64Field(body, "amount"),
	}, nil
}

// FetchOrder reads an order from Razorpay, including its notes and receipt —
// the fields that bind the order to the invoice it was created for.
func (c *RazorpayClient) FetchOrder(orderID string) (*OrderDetails, error) {
	if c == nil || c.orderFetch == nil {
		return nil, ErrNotConfigured
	}
	body, err := c.orderFetch.Fetch(orderID, nil, nil)
	if err != nil {
		return nil, err
	}
	return &OrderDetails{
		ID:       stringField(body, "id"),
		Status:   stringField(body, "status"),
		Receipt:  stringField(body, "receipt"),
		Currency: stringField(body, "currency"),
		Amount:   int64Field(body, "amount"),
		Notes:    stringMapField(body, "notes"),
	}, nil
}

func stringField(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	s, _ := m[key].(string)
	return s
}

func int64Field(m map[string]interface{}, key string) int64 {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	default:
		return 0
	}
}

func stringMapField(m map[string]interface{}, key string) map[string]string {
	out := map[string]string{}
	if m == nil {
		return out
	}
	switch notes := m[key].(type) {
	case map[string]interface{}:
		for k, v := range notes {
			if s, ok := v.(string); ok {
				out[k] = s
			}
		}
	case map[string]string:
		for k, v := range notes {
			out[k] = v
		}
	}
	return out
}
