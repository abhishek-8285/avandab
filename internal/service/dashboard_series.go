package service

import (
	"time"

	"transport-app/internal/repository"
)

// dashboardSeriesWindow is the trailing window the dashboard charts render.
const dashboardSeriesWindow = 30

// ZeroFillRevenueByDay returns one point per day for the trailing 30-day
// window ending today, filling days with no payments with zero. Without
// this the line chart joins sparse points and misleads about shape.
func ZeroFillRevenueByDay(rows []repository.RevenueByDay, today time.Time) []repository.RevenueByDay {
	byDay := make(map[string]float64, len(rows))
	for _, r := range rows {
		byDay[r.Day] += r.Total
	}
	out := make([]repository.RevenueByDay, 0, dashboardSeriesWindow)
	for i := dashboardSeriesWindow - 1; i >= 0; i-- {
		day := today.AddDate(0, 0, -i).Format("2006-01-02")
		out = append(out, repository.RevenueByDay{Day: day, Total: byDay[day]})
	}
	return out
}

// ZeroFillBookingsByDay is the bookings counterpart (counts, not money).
func ZeroFillBookingsByDay(rows []repository.BookingsByDay, today time.Time) []repository.BookingsByDay {
	byDay := make(map[string]int64, len(rows))
	for _, r := range rows {
		byDay[r.Day] += r.Count
	}
	out := make([]repository.BookingsByDay, 0, dashboardSeriesWindow)
	for i := dashboardSeriesWindow - 1; i >= 0; i-- {
		day := today.AddDate(0, 0, -i).Format("2006-01-02")
		out = append(out, repository.BookingsByDay{Day: day, Count: byDay[day]})
	}
	return out
}
