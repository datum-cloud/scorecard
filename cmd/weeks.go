package cmd

import "time"

// Week boundaries are Monday 00:00:00 UTC to Sunday 23:59:59 UTC.
// Reports show only completed weeks - if run mid-week, the most recent
// week shown is the one that ended on the previous Sunday.

// getWeekStart returns the Monday (start) of the week containing time t.
// The returned string is in "2006-01-02" format.
func getWeekStart(t time.Time) string {
	// Convert to UTC for consistent week boundaries
	t = t.UTC()

	// Get Monday of the week (weekday 0 = Sunday, 1 = Monday, ..., 6 = Saturday)
	weekday := int(t.Weekday())
	if weekday == 0 {
		weekday = 7 // Treat Sunday as day 7
	}
	monday := t.AddDate(0, 0, -(weekday - 1))
	return monday.Format("2006-01-02")
}

// getLastCompletedWeekStart returns the Monday of the most recently completed week.
// A week is considered complete when Sunday 23:59:59 UTC has passed.
func getLastCompletedWeekStart() string {
	now := time.Now().UTC()

	// Find the most recent Sunday that has fully passed
	weekday := int(now.Weekday())
	var lastSunday time.Time
	if weekday == 0 {
		// Today is Sunday - the last completed week ended last Sunday
		lastSunday = now.AddDate(0, 0, -7)
	} else {
		// Go back to the most recent Sunday
		lastSunday = now.AddDate(0, 0, -weekday)
	}

	// The week that ended on lastSunday started on the Monday before
	monday := lastSunday.AddDate(0, 0, -6)
	return monday.Format("2006-01-02")
}

// getLastNWeeks returns the last N completed weeks, oldest first.
// Each entry is the Monday (start date) of that week in "2006-01-02" format.
func getLastNWeeks(n int) []string {
	lastWeekStart := getLastCompletedWeekStart()
	t, _ := time.Parse("2006-01-02", lastWeekStart)

	weeks := make([]string, n)
	for i := 0; i < n; i++ {
		weeks[n-1-i] = t.Format("2006-01-02")
		t = t.AddDate(0, 0, -7)
	}
	return weeks
}

// getLast4Weeks returns the last 4 completed weeks, oldest first.
func getLast4Weeks() []string {
	return getLastNWeeks(4)
}

// getLast26Weeks returns the last 26 completed weeks (6 months), oldest first.
func getLast26Weeks() []string {
	return getLastNWeeks(26)
}

// weekStartToEnd converts a Monday date string to the corresponding Sunday date string.
// Input and output are in "2006-01-02" format.
func weekStartToEnd(monday string) string {
	t, _ := time.Parse("2006-01-02", monday)
	sunday := t.AddDate(0, 0, 6)
	return sunday.Format("2006-01-02")
}

// formatWeekEnd formats a Monday date string as the corresponding Sunday in "Jan 02" format.
func formatWeekEnd(monday string) string {
	t, _ := time.Parse("2006-01-02", monday)
	sunday := t.AddDate(0, 0, 6)
	return sunday.Format("Jan 02")
}
