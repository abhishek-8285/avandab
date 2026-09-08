package comm

import (
	"fmt"
	"os"
	"strings"
)

// publicBaseURL is the customer-facing origin for links sent over WhatsApp.
// Configurable via PUBLIC_BASE_URL so custom deployments don't point
// customers at avandab.com.
func publicBaseURL() string {
	if base := strings.TrimSpace(os.Getenv("PUBLIC_BASE_URL")); base != "" {
		return strings.TrimRight(base, "/")
	}
	return "https://avandab.com"
}

// FormatTripDispatchMessage formats the driver notification message when a trip is dispatched.
func FormatTripDispatchMessage(origin, destination, vehicleID string) string {
	if origin == "" {
		origin = "Origin"
	}
	if destination == "" {
		destination = "Destination"
	}
	return fmt.Sprintf("🚚 Avandab Trip Dispatched: %s ➔ %s | Live Tracking: %s/tracking#v=%s",
		origin, destination, publicBaseURL(), vehicleID)
}

// FormatTripTrackingMessage formats customer tracking update when a trip starts.
// The link targets the customer portal page (/customer/tracking/{trip_id}),
// never the staff-only /tracking map (which bounces customers to /login).
func FormatTripTrackingMessage(tripID, tripNumber, origin, destination string) string {
	trackingURL := fmt.Sprintf("%s/customer/tracking/%s", publicBaseURL(), tripID)
	if origin != "" && destination != "" {
		return fmt.Sprintf("🚚 Avandab Shipment On The Way: Trip #%s (%s ➔ %s) has departed. Live Tracking: %s",
			tripNumber, origin, destination, trackingURL)
	}
	return fmt.Sprintf("🚚 Avandab Shipment On The Way: Trip #%s has departed. Live Tracking: %s",
		tripNumber, trackingURL)
}

// FormatBookingConfirmedMessage formats customer notification when a booking is confirmed.
func FormatBookingConfirmedMessage(bookingNumber, origin, destination, vehicleOrURL string) string {
	trackingURL := vehicleOrURL
	if !strings.HasPrefix(trackingURL, "http://") && !strings.HasPrefix(trackingURL, "https://") {
		if trackingURL != "" {
			trackingURL = fmt.Sprintf("%s/tracking#v=%s", publicBaseURL(), trackingURL)
		} else {
			trackingURL = fmt.Sprintf("%s/tracking#b=%s", publicBaseURL(), bookingNumber)
		}
	}
	if origin != "" && destination != "" {
		return fmt.Sprintf("📦 Avandab Booking Confirmed: #%s (%s ➔ %s). Track your shipment live: %s",
			bookingNumber, origin, destination, trackingURL)
	}
	return fmt.Sprintf("📦 Avandab Booking Confirmed: #%s. Track your shipment live: %s",
		bookingNumber, trackingURL)
}

// FormatPODReceiptMessage formats delivery completion notification with digital e-POD link.
func FormatPODReceiptMessage(tripNumber, podURL string) string {
	if podURL != "" {
		return fmt.Sprintf("✅ Avandab Delivery Completed: Trip #%s has been delivered. View your digital e-POD receipt: %s",
			tripNumber, podURL)
	}
	return fmt.Sprintf("✅ Avandab Delivery Completed: Trip #%s has been delivered. Digital e-POD verified.", tripNumber)
}
