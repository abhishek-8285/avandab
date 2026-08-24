// Package domain re-exports domain entity types and shared types for
// backward compatibility. Entity definitions live in sub-packages:
// domain/trip, domain/booking, domain/driver, domain/vehicle, etc.
package domain

import (
	"transport-app/internal/domain/audit"
	"transport-app/internal/domain/booking"
	"transport-app/internal/domain/company"
	"transport-app/internal/domain/customer"
	"transport-app/internal/domain/dispatch"
	"transport-app/internal/domain/driver"
	"transport-app/internal/domain/invoice"
	"transport-app/internal/domain/payment"
	"transport-app/internal/domain/route"
	"transport-app/internal/domain/trip"
	"transport-app/internal/domain/types"
	"transport-app/internal/domain/user"
	"transport-app/internal/domain/vehicle"
)

// Shared type aliases (IDs, File, Session)
type (
	UserID     = types.UserID
	DriverID   = types.DriverID
	VehicleID  = types.VehicleID
	CustomerID = types.CustomerID
	RouteID    = types.RouteID
	BookingID  = types.BookingID
	TripID     = types.TripID
	InvoiceID  = types.InvoiceID
	PaymentID  = types.PaymentID
	FileID     = types.FileID
	SessionID  = types.SessionID

	File    = types.File
	Session = types.Session
)

// Entity type aliases
type (
	CompanySettings = company.CompanySettings

	User       = user.User
	Role       = user.Role
	UserStatus = user.UserStatus
	RoleName   = user.RoleName

	Driver       = driver.Driver
	DriverStatus = driver.DriverStatus

	Vehicle       = vehicle.Vehicle
	VehicleType   = vehicle.VehicleType
	FuelType      = vehicle.FuelType
	VehicleStatus = vehicle.VehicleStatus

	Customer = customer.Customer

	Route = route.Route

	CreateRouteRequest = route.CreateRouteRequest
	UpdateRouteRequest = route.UpdateRouteRequest

	Booking       = booking.Booking
	BookingStatus = booking.BookingStatus

	Dispatch       = dispatch.Dispatch
	DispatchStatus = dispatch.DispatchStatus

	Trip       = trip.Trip
	TripStatus = trip.TripStatus

	Invoice       = invoice.Invoice
	InvoiceStatus = invoice.InvoiceStatus
	PaymentStatus = invoice.PaymentStatus

	Payment       = payment.Payment
	PaymentMethod = payment.PaymentMethod

	AuditLog = audit.AuditLog
)

// Entity type aliases (continued - cross-package references)
// AuditLog references FileID and UserID from shared types

// Status constant aliases
const (
	// company
	// (none)

	// user
	RoleAdmin           = user.RoleAdmin
	RoleOrgAdmin        = user.RoleOrgAdmin
	RoleDispatcher      = user.RoleDispatcher
	RoleAccountant      = user.RoleAccountant
	RoleViewer          = user.RoleViewer
	RoleDriver          = user.RoleDriver
	RoleCustomer        = user.RoleCustomer
	UserStatusActive    = user.UserStatusActive
	UserStatusInactive  = user.UserStatusInactive
	UserStatusSuspended = user.UserStatusSuspended

	// driver
	DriverAvailable = driver.DriverAvailable
	DriverOnTrip    = driver.DriverOnTrip
	DriverLeave     = driver.DriverLeave
	DriverInactive  = driver.DriverInactive
	DriverBlocked   = driver.DriverBlocked

	// vehicle
	VehicleTypeTruck     = vehicle.VehicleTypeTruck
	VehicleTypeMiniTruck = vehicle.VehicleTypeMiniTruck
	VehicleTypeBus       = vehicle.VehicleTypeBus
	VehicleTypeVan       = vehicle.VehicleTypeVan
	VehicleTypePickup    = vehicle.VehicleTypePickup
	VehicleTypeTempo     = vehicle.VehicleTypeTempo
	FuelTypeDiesel       = vehicle.FuelTypeDiesel
	FuelTypePetrol       = vehicle.FuelTypePetrol
	FuelTypeGas          = vehicle.FuelTypeGas
	FuelTypeElectric     = vehicle.FuelTypeElectric
	FuelTypeCNG          = vehicle.FuelTypeCNG
	VehicleAvailable     = vehicle.VehicleAvailable
	VehicleRunning       = vehicle.VehicleRunning
	VehicleMaintenance   = vehicle.VehicleMaintenance
	VehicleInactive      = vehicle.VehicleInactive
	VehicleBlocked       = vehicle.VehicleBlocked

	// booking
	BookingDraft     = booking.BookingDraft
	BookingPending   = booking.BookingPending
	BookingConfirmed = booking.BookingConfirmed
	BookingCancelled = booking.BookingCancelled
	BookingCompleted = booking.BookingCompleted

	// dispatch
	DispatchDraft     = dispatch.DispatchDraft
	DispatchAssigned  = dispatch.DispatchAssigned
	DispatchConverted = dispatch.DispatchConverted
	DispatchCancelled = dispatch.DispatchCancelled

	// trip
	TripDraft     = trip.TripDraft
	TripScheduled = trip.TripScheduled
	TripAssigned  = trip.TripAssigned
	TripStarted   = trip.TripStarted
	TripInTransit = trip.TripInTransit
	TripDelivered = trip.TripDelivered
	TripCompleted = trip.TripCompleted
	TripCancelled = trip.TripCancelled

	// invoice
	InvoiceDraft       = invoice.InvoiceDraft
	InvoiceIssued      = invoice.InvoiceIssued
	InvoiceOutstanding = invoice.InvoiceOutstanding
	InvoicePaid        = invoice.InvoicePaid
	InvoiceCancelled   = invoice.InvoiceCancelled

	PaymentStatusPending       = invoice.PaymentStatusPending
	PaymentStatusPaid          = invoice.PaymentStatusPaid
	PaymentStatusPartiallyPaid = invoice.PaymentStatusPartiallyPaid

	// payment
	PaymentMethodCash         = payment.PaymentMethodCash
	PaymentMethodUPI          = payment.PaymentMethodUPI
	PaymentMethodBankTransfer = payment.PaymentMethodBankTransfer
	PaymentMethodCheque       = payment.PaymentMethodCheque
	PaymentMethodRazorpay     = payment.PaymentMethodRazorpay
)

// ActiveTripStatuses alias
var ActiveTripStatuses = trip.ActiveTripStatuses

// DefaultRoleID re-exports the function from the user package.
var DefaultRoleID = user.DefaultRoleID

// Error aliases (backward compatibility)
var (
	// Auth errors
	ErrInvalidCredentials = user.ErrInvalidCredentials
	ErrUserNotFound       = user.ErrUserNotFound
	ErrUserEmailExists    = user.ErrUserEmailExists
	ErrUnauthorized       = user.ErrUnauthorized
	ErrSessionExpired     = user.ErrSessionExpired
	ErrUserEmailRequired  = user.ErrUserEmailRequired
	ErrUserPhoneRequired  = user.ErrUserPhoneRequired
	ErrWeakPassword       = user.ErrWeakPassword

	// Entity errors
	ErrDriverNotFound            = driver.ErrDriverNotFound
	ErrDriverUnavailable         = driver.ErrDriverUnavailable
	ErrDriverOnTrip              = driver.ErrDriverOnTrip
	ErrVehicleNotFound           = vehicle.ErrVehicleNotFound
	ErrVehicleUnavailable        = vehicle.ErrVehicleUnavailable
	ErrVehicleAssigned           = vehicle.ErrVehicleAssigned
	ErrVehicleMaintenanceBlocked = vehicle.ErrVehicleMaintenanceBlocked
	ErrCustomerNotFound          = customer.ErrCustomerNotFound
	ErrRouteNotFound             = route.ErrRouteNotFound
	ErrBookingNotFound           = booking.ErrBookingNotFound
	ErrDispatchNotFound          = dispatch.ErrDispatchNotFound
	ErrTripNotFound              = trip.ErrTripNotFound
	ErrInvoiceNotFound           = invoice.ErrInvoiceNotFound
	ErrPaymentNotFound           = payment.ErrPaymentNotFound

	// Business rule errors
	ErrTripImmutable          = trip.ErrTripImmutable
	ErrCancelledTripImmutable = trip.ErrCancelledTripImmutable
	ErrCompletedTripImmutable = trip.ErrCompletedTripImmutable
	ErrDuplicateInvoice       = invoice.ErrDuplicateInvoice
)
