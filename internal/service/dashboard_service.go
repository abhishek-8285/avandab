package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"transport-app/internal/domain"
	"transport-app/internal/repository"
)

// DashboardData contains all the data needed to render the dashboard.
type DashboardData struct {
	// Cards
	TodaysTripsCount       int64
	ActiveTripsCount       int64
	CompletedTripsCount    int64
	CancelledTripsCount    int64
	AvailableVehiclesCount int64
	AvailableDriversCount  int64
	PendingPaymentsCount   int
	MonthlyRevenue         float64

	// Charts (variant B)
	RevenueSeries  []repository.MonthlyRevenue
	RevenueByDay   []repository.RevenueByDay
	BookingsByDay  []repository.BookingsByDay
	StatusCounts   map[domain.TripStatus]int64
	DeltaYesterday int64

	// Alerts (variant B)
	OverdueTrips []repository.TripWithJoins
	IdleVehicles []domain.Vehicle

	// Exception strip (variant B): actionable backlogs, all tenant-scoped.
	Attention AttentionCounts

	// Tables
	UpcomingTrips   []repository.TripWithJoins
	RecentBookings  []repository.BookingWithJoins
	RecentPayments  []repository.PaymentWithInvoice
	PendingInvoices []repository.InvoiceWithJoins
	RecentActivity  []repository.AuditLogWithUser
}

// FastagLowBalanceKey is the company_config key overriding the low-balance
// attention threshold (INR) per tenant. Missing/invalid/non-positive values
// fall back to DefaultLowFastagThreshold. Dotted-key convention follows the
// Spec 24 overlay (cf. billing.gst_rate).
const FastagLowBalanceKey = "fastag.low_balance_threshold"

// DefaultLowFastagThreshold is the wallet balance (INR) below which a FASTag
// tag needs attention when the tenant sets no override.
const DefaultLowFastagThreshold = 500.0

// AttentionCounts is one tenant's exception-strip snapshot.
type AttentionCounts struct {
	UnassignedBookings int64
	MaintenanceDue     int64
	OpenWorkOrders     int64
	GarageVehicles     int64
	OpenAlerts         int64
	ActiveDTCs         int64
	ExpiringEwaybills  int64
	PendingKharcha     int64
	LowFastag          int64
}

// Total sums every backlog item for the strip's show/hide gate.
func (a AttentionCounts) Total() int64 {
	return a.UnassignedBookings + a.MaintenanceDue + a.OpenWorkOrders +
		a.GarageVehicles + a.OpenAlerts + a.ActiveDTCs + a.ExpiringEwaybills +
		a.PendingKharcha + a.LowFastag
}

// dashboardCacheEntry is one tenant's snapshot of the aggregated dashboard.
type dashboardCacheEntry struct {
	data DashboardData
	at   time.Time
}

// DashboardService provides dashboard data aggregation with high-performance memory caching.
// The cache is keyed by tenant so one tenant's numbers are never served to
// another (cross-tenant serve was a leak under the single-entry cache).
type DashboardService struct {
	baseService
	cacheMu sync.RWMutex
	cache   map[string]*dashboardCacheEntry
	ttl     time.Duration
}

// GetDashboardData returns aggregated data for the dashboard with ultra-fast memory caching.
func (s *DashboardService) GetDashboardData(ctx context.Context) (DashboardData, error) {
	ttl := s.ttl
	if ttl == 0 {
		ttl = 3 * time.Second
	}
	tenantKey := tenantIDFor(ctx)

	s.cacheMu.RLock()
	if entry, ok := s.cache[tenantKey]; ok && time.Since(entry.at) < ttl {
		data := entry.data
		s.cacheMu.RUnlock()
		return data, nil
	}
	s.cacheMu.RUnlock()

	var (
		todaysTripsCount, activeTripsCount, completedTripsCount, cancelledTripsCount int64
		availVehiclesCount, availDriversCount                                        int64
		pendingPaymentsCount                                                         int
		monthlyRevenue                                                               float64
		upcomingTrips                                                                []repository.TripWithJoins
		recentBookings                                                               []repository.BookingWithJoins
		recentPayments                                                               []repository.PaymentWithInvoice
		pendingInvoices                                                              []repository.InvoiceWithJoins
		recentActivity                                                               []repository.AuditLogWithUser
		statusCounts                                                                 map[domain.TripStatus]int64
		revenueSeries                                                                []repository.MonthlyRevenue
		revenueByDay                                                                 []repository.RevenueByDay
		bookingsByDay                                                                []repository.BookingsByDay
		overdueTrips                                                                 []repository.TripWithJoins
		idleVehicles                                                                 []domain.Vehicle
		yesterdayCount                                                               int64
		attention                                                                    AttentionCounts
	)

	today := time.Now().Format("2006-01-02")
	todayTime := time.Now()
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	g, ctx := errgroup.WithContext(ctx)

	// 1. Today's trips by status
	g.Go(func() error {
		counts, err := s.store.CountTripsByStatusForDate(ctx, today)
		if err == nil {
			statusCounts = counts
			completedTripsCount = counts[domain.TripCompleted]
			cancelledTripsCount = counts[domain.TripCancelled]
			// Sum every status the DB returns: en-route states (in_transit,
			// delivered, reached_pickup) must not vanish from the board.
			// Active = anything neither terminal nor draft.
			for _, n := range counts {
				todaysTripsCount += n
			}
			activeTripsCount = todaysTripsCount - completedTripsCount -
				cancelledTripsCount - counts[domain.TripDraft]
		}
		return nil
	})

	// 2. Available vehicles
	g.Go(func() error {
		vehicles, err := s.store.GetAvailableVehicles(ctx)
		if err == nil {
			availVehiclesCount = int64(len(vehicles))
		}
		return nil
	})

	// 3. Available drivers
	g.Go(func() error {
		drivers, err := s.store.GetAvailableDrivers(ctx)
		if err == nil {
			availDriversCount = int64(len(drivers))
		}
		return nil
	})

	// 4. Pending invoices (count + oldest-first rows for the alert feed)
	g.Go(func() error {
		pending, err := s.store.GetPendingInvoices(ctx)
		if err == nil {
			pendingPaymentsCount = len(pending)
			if len(pending) > 10 {
				pending = pending[:10]
			}
			pendingInvoices = pending
		}
		return nil
	})

	// 5. Monthly revenue
	g.Go(func() error {
		monthlyRev, err := s.store.GetMonthlyRevenue(ctx)
		if err == nil {
			revenueSeries = monthlyRev
			currentMonth := time.Now().Format("2006-01")
			for _, rev := range monthlyRev {
				if rev.Month == currentMonth {
					monthlyRevenue = rev.Total
					break
				}
			}
		}
		return nil
	})

	// 10. Revenue by day (last 30 days, for the area chart; zero-filled)
	g.Go(func() error {
		rows, err := s.store.GetRevenueByDay(ctx)
		if err == nil {
			revenueByDay = ZeroFillRevenueByDay(rows, todayTime)
		} else {
			revenueByDay = ZeroFillRevenueByDay(nil, todayTime)
		}
		return nil
	})

	// 11. Bookings by day (last 30 days, for the bar chart; zero-filled)
	g.Go(func() error {
		rows, err := s.store.CountBookingsByDay(ctx)
		if err == nil {
			bookingsByDay = ZeroFillBookingsByDay(rows, todayTime)
		} else {
			bookingsByDay = ZeroFillBookingsByDay(nil, todayTime)
		}
		return nil
	})

	// 12. Overdue trips (alert feed)
	g.Go(func() error {
		trips, err := s.store.GetOverdueTrips(ctx)
		if err == nil {
			overdueTrips = trips
		} else {
			overdueTrips = []repository.TripWithJoins{}
		}
		return nil
	})

	// 13. Idle vehicles (alert feed)
	g.Go(func() error {
		vehicles, err := s.store.GetIdleVehicles(ctx)
		if err == nil {
			idleVehicles = vehicles
		} else {
			idleVehicles = []domain.Vehicle{}
		}
		return nil
	})

	// 14. Yesterday's trip count (for the delta chip; sum-all, same as today)
	g.Go(func() error {
		counts, err := s.store.CountTripsByStatusForDate(ctx, yesterday)
		if err == nil {
			for _, n := range counts {
				yesterdayCount += n
			}
		}
		return nil
	})

	// 15. Exception strip (each failure degrades to 0, like the rest)
	g.Go(func() error {
		if n, err := s.store.CountUnassignedBookings(ctx); err == nil {
			attention.UnassignedBookings = n
		}
		return nil
	})
	g.Go(func() error {
		if n, err := s.store.CountMaintenanceDue(ctx); err == nil {
			attention.MaintenanceDue = n
		}
		if n, err := s.store.CountOpenWorkOrders(ctx); err == nil {
			attention.OpenWorkOrders = n
		}
		if n, err := s.store.CountGarageVehicles(ctx); err == nil {
			attention.GarageVehicles = n
		}
		return nil
	})
	g.Go(func() error {
		if n, err := s.store.CountOpenAlerts(ctx); err == nil {
			attention.OpenAlerts = n
		}
		if n, err := s.store.CountActiveDTCs(ctx); err == nil {
			attention.ActiveDTCs = n
		}
		return nil
	})
	g.Go(func() error {
		if n, err := s.store.CountExpiringEwaybills(ctx); err == nil {
			attention.ExpiringEwaybills = n
		}
		if n, err := s.store.CountPendingKharcha(ctx); err == nil {
			attention.PendingKharcha = n
		}
		// Per-tenant low-balance threshold with safe default. tenantCfg is
		// nil-safe (tests/fakes without a raw DB fall through to default).
		threshold := DefaultLowFastagThreshold
		if v := s.tenantCfg.GetFloat(ctx, tenantIDFor(ctx), FastagLowBalanceKey, DefaultLowFastagThreshold); v > 0 {
			threshold = v
		}
		if n, err := s.store.CountLowFastag(ctx, threshold); err == nil {
			attention.LowFastag = n
		}
		return nil
	})
	g.Go(func() error {
		counts, err := s.store.CountTripsByStatusForDate(ctx, yesterday)
		if err == nil {
			for _, n := range counts {
				yesterdayCount += n
			}
		}
		return nil
	})

	// 6. Upcoming trips
	g.Go(func() error {
		trips, err := s.store.GetTripsByDate(ctx, today)
		if err == nil {
			upcomingTrips = trips
		} else {
			upcomingTrips = []repository.TripWithJoins{}
		}
		return nil
	})

	// 7. Recent bookings
	g.Go(func() error {
		bookings, err := s.store.SearchBookings(ctx, "", "", 10, 0)
		if err == nil {
			recentBookings = bookings
		} else {
			recentBookings = []repository.BookingWithJoins{}
		}
		return nil
	})

	// 8. Recent payments
	g.Go(func() error {
		payments, err := s.store.SearchPayments(ctx, "", 10, 0)
		if err == nil {
			recentPayments = payments
		} else {
			recentPayments = []repository.PaymentWithInvoice{}
		}
		return nil
	})

	// 9. Recent activity
	g.Go(func() error {
		logs, err := s.store.ListAuditLogs(ctx, 10, 0)
		if err == nil {
			recentActivity = logs
		} else {
			recentActivity = []repository.AuditLogWithUser{}
		}
		return nil
	})

	_ = g.Wait()

	data := DashboardData{
		TodaysTripsCount:       todaysTripsCount,
		ActiveTripsCount:       activeTripsCount,
		CompletedTripsCount:    completedTripsCount,
		CancelledTripsCount:    cancelledTripsCount,
		AvailableVehiclesCount: availVehiclesCount,
		AvailableDriversCount:  availDriversCount,
		PendingPaymentsCount:   pendingPaymentsCount,
		MonthlyRevenue:         monthlyRevenue,
		RevenueSeries:          revenueSeries,
		RevenueByDay:           revenueByDay,
		BookingsByDay:          bookingsByDay,
		StatusCounts:           statusCounts,
		DeltaYesterday:         todaysTripsCount - yesterdayCount,
		OverdueTrips:           overdueTrips,
		IdleVehicles:           idleVehicles,
		Attention:              attention,
		UpcomingTrips:          upcomingTrips,
		RecentBookings:         recentBookings,
		RecentPayments:         recentPayments,
		PendingInvoices:        pendingInvoices,
		RecentActivity:         recentActivity,
	}

	s.cacheMu.Lock()
	if s.cache == nil {
		s.cache = make(map[string]*dashboardCacheEntry)
	}
	s.cache[tenantKey] = &dashboardCacheEntry{data: data, at: time.Now()}
	s.cacheMu.Unlock()

	return data, nil
}

// GetUpcomingTrips returns trips starting today.
func (s *DashboardService) GetUpcomingTrips(ctx context.Context, date string) ([]repository.TripWithJoins, error) {
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}
	return s.store.GetTripsByDate(ctx, date)
}

// GetAvailableDriversForDashboard returns available drivers count.
func (s *DashboardService) GetAvailableDriversForDashboard(ctx context.Context) (int64, error) {
	drivers, err := s.store.GetAvailableDrivers(ctx)
	if err != nil {
		return 0, err
	}
	return int64(len(drivers)), nil
}

// GetAvailableVehiclesForDashboard returns available vehicles count.
func (s *DashboardService) GetAvailableVehiclesForDashboard(ctx context.Context) (int64, error) {
	vehicles, err := s.store.GetAvailableVehicles(ctx)
	if err != nil {
		return 0, err
	}
	return int64(len(vehicles)), nil
}

// GetTodayTripsSummary returns counts of trips by status for a given date.
func (s *DashboardService) GetTodayTripsSummary(ctx context.Context, date string) (map[domain.TripStatus]int64, error) {
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}
	return s.store.CountTripsByStatusForDate(ctx, date)
}

// GetPendingPaymentsCount returns the count of invoices with pending/partial payments.
func (s *DashboardService) GetPendingPaymentsCount(ctx context.Context) (int, error) {
	pending, err := s.store.GetPendingInvoices(ctx)
	if err != nil {
		return 0, err
	}
	return len(pending), nil
}

// GetMonthlyRevenueSummary returns the total revenue for the current month.
func (s *DashboardService) GetMonthlyRevenueSummary(ctx context.Context) (float64, error) {
	monthlyRev, err := s.store.GetMonthlyRevenue(ctx)
	if err != nil {
		return 0, err
	}
	currentMonth := time.Now().Format("2006-01")
	for _, rev := range monthlyRev {
		if rev.Month == currentMonth {
			return rev.Total, nil
		}
	}
	return 0, nil
}

func (s *DashboardService) GetStats(ctx context.Context) (map[string]int64, error) {
	stats := make(map[string]int64)
	today := time.Now().Format("2006-01-02")

	statusCounts, err := s.store.CountTripsByStatusForDate(ctx, today)
	if err != nil {
		return nil, fmt.Errorf("failed to get trip stats: %w", err)
	}

	for status, count := range statusCounts {
		stats[string(status)] = count
	}

	vehicles, _ := s.store.GetAvailableVehicles(ctx)
	stats["available_vehicles"] = int64(len(vehicles))

	drivers, _ := s.store.GetAvailableDrivers(ctx)
	stats["available_drivers"] = int64(len(drivers))

	return stats, nil
}
