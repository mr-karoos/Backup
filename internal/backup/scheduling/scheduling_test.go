package scheduling

import (
	"errors"
	"testing"
	"time"
)

func TestValidateTimezone(t *testing.T) {
	validCases := []string{
		"UTC",
		"Asia/Tehran",
		"Europe/Berlin",
		"America/New_York",
		"Australia/Brisbane",
	}

	for _, tz := range validCases {
		t.Run("Valid timezone "+tz, func(t *testing.T) {
			loc, err := ValidateTimezone(tz)
			if err != nil {
				t.Fatalf("expected valid timezone %s, got error: %v", tz, err)
			}
			if loc == nil {
				t.Fatalf("expected non-nil location for %s", tz)
			}
		})
	}

	invalidCases := []string{
		"",
		"   ",
		"Mars/Olympus",
		"Invalid/Timezone",
		"UTC+3",
	}

	for _, tz := range invalidCases {
		t.Run("Invalid timezone "+tz, func(t *testing.T) {
			loc, err := ValidateTimezone(tz)
			if err == nil {
				t.Fatalf("expected error for invalid timezone %q, got nil (loc: %v)", tz, loc)
			}
			if !errors.Is(err, ErrInvalidTimezone) {
				t.Fatalf("expected ErrInvalidTimezone, got: %v", err)
			}
		})
	}
}

func TestParseSchedule_ValidCron(t *testing.T) {
	validExpressions := []struct {
		name string
		expr string
		tz   string
	}{
		{name: "Every midnight", expr: "0 0 * * *", tz: "UTC"},
		{name: "Daily at 02:00", expr: "0 2 * * *", tz: "Asia/Tehran"},
		{name: "Every 15 minutes", expr: "*/15 * * * *", tz: "Europe/Berlin"},
		{name: "Weekdays at 04:30", expr: "30 4 * * 1-5", tz: "America/New_York"},
		{name: "First of month at midnight", expr: "0 0 1 * *", tz: "UTC"},
		{name: "Quarterly first day", expr: "0 0 1 1,4,7,10 *", tz: "UTC"},
	}

	for _, tc := range validExpressions {
		t.Run(tc.name, func(t *testing.T) {
			sched, loc, err := ParseSchedule(tc.expr, tc.tz)
			if err != nil {
				t.Fatalf("expected valid schedule for %q in %s, got error: %v", tc.expr, tc.tz, err)
			}
			if sched == nil || loc == nil {
				t.Fatalf("expected non-nil schedule and location")
			}
		})
	}
}

func TestParseSchedule_InvalidCron(t *testing.T) {
	invalidCases := []struct {
		name string
		expr string
	}{
		{name: "Empty expression", expr: ""},
		{name: "Whitespace only", expr: "   "},
		{name: "Too few fields (4 fields)", expr: "0 2 * *"},
		{name: "Too many fields (6 fields with seconds)", expr: "0 0 2 * * *"},
		{name: "7 fields", expr: "0 0 0 2 * * *"},
		{name: "Invalid minute (99)", expr: "99 2 * * *"},
		{name: "Invalid hour (25)", expr: "0 25 * * *"},
		{name: "Invalid day of month (32)", expr: "0 2 32 * *"},
		{name: "Invalid month (13)", expr: "0 2 1 13 *"},
		{name: "Invalid day of week (8)", expr: "0 2 * * 8"},
		{name: "Malformed text token", expr: "abc 2 * * *"},
		{name: "Descriptor @daily", expr: "@daily"},
		{name: "Descriptor @hourly", expr: "@hourly"},
		{name: "Descriptor @every", expr: "@every 1h"},
		{name: "Descriptor @reboot", expr: "@reboot"},
	}

	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			sched, loc, err := ParseSchedule(tc.expr, "UTC")
			if err == nil {
				t.Fatalf("expected error for invalid expression %q, got nil (sched: %v, loc: %v)", tc.expr, sched, loc)
			}
			if !errors.Is(err, ErrInvalidCronExpression) {
				t.Fatalf("expected ErrInvalidCronExpression for %q, got: %v", tc.expr, err)
			}
		})
	}
}

func TestCalculateNextRun_UTC(t *testing.T) {
	// Schedule: daily at 02:00
	expr := "0 2 * * *"
	tz := "UTC"

	t.Run("Before 02:00 on same day returns 02:00 same day", func(t *testing.T) {
		after := time.Date(2026, 8, 20, 1, 0, 0, 0, time.UTC)
		next, err := CalculateNextRun(expr, tz, after)
		if err != nil {
			t.Fatalf("CalculateNextRun failed: %v", err)
		}
		expected := time.Date(2026, 8, 20, 2, 0, 0, 0, time.UTC)
		if !next.Equal(expected) {
			t.Fatalf("expected %v, got %v", expected, *next)
		}
	})

	t.Run("After 02:00 on same day returns 02:00 next day", func(t *testing.T) {
		after := time.Date(2026, 8, 20, 2, 30, 0, 0, time.UTC)
		next, err := CalculateNextRun(expr, tz, after)
		if err != nil {
			t.Fatalf("CalculateNextRun failed: %v", err)
		}
		expected := time.Date(2026, 8, 21, 2, 0, 0, 0, time.UTC)
		if !next.Equal(expected) {
			t.Fatalf("expected %v, got %v", expected, *next)
		}
	})
}

func TestCalculateNextRun_AsiaTehran(t *testing.T) {
	expr := "0 2 * * *"
	tz := "Asia/Tehran"

	loc, err := time.LoadLocation(tz)
	if err != nil {
		t.Fatalf("failed loading location Asia/Tehran: %v", err)
	}

	// 01:00 Tehran time on 2026-08-20
	after := time.Date(2026, 8, 20, 1, 0, 0, 0, loc)
	next, err := CalculateNextRun(expr, tz, after)
	if err != nil {
		t.Fatalf("CalculateNextRun failed: %v", err)
	}

	expectedLocal := time.Date(2026, 8, 20, 2, 0, 0, 0, loc)
	expectedUTC := expectedLocal.UTC()

	if !next.Equal(expectedUTC) {
		t.Fatalf("expected %v (%v), got %v", expectedUTC, expectedLocal, *next)
	}
}

func TestCalculateNextRun_DSTTransition_EuropeBerlin(t *testing.T) {
	// Europe/Berlin transitions to DST (CEST +02:00) on the last Sunday of March
	// In 2026, last Sunday of March is 2026-03-29 (clocks jump from 02:00 to 03:00)
	expr := "0 4 * * *"
	tz := "Europe/Berlin"

	loc, err := time.LoadLocation(tz)
	if err != nil {
		t.Fatalf("failed loading location: %v", err)
	}

	// Before DST change: 2026-03-28 23:00 Berlin time (CET +01:00)
	after := time.Date(2026, 3, 28, 23, 0, 0, 0, loc)
	next, err := CalculateNextRun(expr, tz, after)
	if err != nil {
		t.Fatalf("CalculateNextRun failed: %v", err)
	}

	expectedLocal := time.Date(2026, 3, 29, 4, 0, 0, 0, loc)
	expectedUTC := expectedLocal.UTC()

	if !next.Equal(expectedUTC) {
		t.Fatalf("expected next run %v (%v), got %v", expectedUTC, expectedLocal, *next)
	}
}
