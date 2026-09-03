package comm

import (
	"fmt"
	"strings"
)

// FormatTripDispatchMessage formats the driver notification message when a trip is dispatched.
func FormatTripDispatchMessage(origin, destination, vehicleID string) string {
	if origin == "" {
		origin = "Origin"
	}
	if destination == "" {
		destination = "Destination"
	}
	return fmt.Sprintf("🚚 Avandab Trip Dispatched: %s ➔ %s | Live Tracking: https://avandab.com/tracking#v=%s",
		origin, destination, vehicleID)
}

// FormatTripTrackingMessage formats customer tracking update when a trip starts.
func FormatTripTrackingMessage(tripNumber, origin, destination, vehicleID string) string {
	trackingURL := fmt.Sprintf("https://avandab.com/tracking#v=%s", vehicleID)
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
			trackingURL = fmt.Sprintf("https://avandab.com/tracking#v=%s", trackingURL)
		} else {
			trackingURL = fmt.Sprintf("https://avandab.com/tracking#b=%s", bookingNumber)
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
