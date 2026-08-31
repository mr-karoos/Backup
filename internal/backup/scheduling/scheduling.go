package scheduling

import (
	"errors"
	"fmt"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/robfig/cron/v3"
)

var (
	// ErrInvalidCronExpression is returned when the cron expression is malformed, not 5 fields, or uses unsupported descriptors.
	ErrInvalidCronExpression = errors.New("invalid 5-field cron expression")

	// ErrInvalidTimezone is returned when the timezone string is not a valid IANA timezone location.
	ErrInvalidTimezone = errors.New("invalid IANA timezone")
)

var standardCronParser = cron.NewParser(
	cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow,
)

// ValidateTimezone verifies that the timezone string represents a valid IANA timezone.
func ValidateTimezone(tz string) (*time.Location, error) {
	trimmed := strings.TrimSpace(tz)
	if trimmed == "" {
		return nil, ErrInvalidTimezone
	}
	loc, err := time.LoadLocation(trimmed)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidTimezone, trimmed)
	}
	return loc, nil
}

// ParseSchedule validates the 5-field cron expression and timezone, returning the Schedule and Location.
func ParseSchedule(cronExpr, timezone string) (cron.Schedule, *time.Location, error) {
	loc, err := ValidateTimezone(timezone)
	if err != nil {
		return nil, nil, err
	}

	trimmedExpr := strings.TrimSpace(cronExpr)
	if trimmedExpr == "" {
		return nil, nil, ErrInvalidCronExpression
	}

	// Reject cron descriptors (@every, @daily, @hourly, @reboot, etc.)
	if strings.HasPrefix(trimmedExpr, "@") {
		return nil, nil, fmt.Errorf("%w: cron descriptors are not supported", ErrInvalidCronExpression)
	}

	// Enforce exactly 5 whitespace-separated fields
	fields := strings.Fields(trimmedExpr)
	if len(fields) != 5 {
		return nil, nil, fmt.Errorf("%w: expected exactly 5 fields (minute, hour, dom, month, dow), got %d", ErrInvalidCronExpression, len(fields))
	}

	schedule, err := standardCronParser.Parse(trimmedExpr)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrInvalidCronExpression, err)
	}

	return schedule, loc, nil
}

// CalculateNextRun computes the next scheduled occurrence strictly after the given timestamp in the specified timezone.
// The result is returned as an absolute UTC timestamp for database persistence.
func CalculateNextRun(cronExpr, timezone string, after time.Time) (*time.Time, error) {
	schedule, loc, err := ParseSchedule(cronExpr, timezone)
	if err != nil {
		return nil, err
	}

	localAfter := after.In(loc)
	nextLocal := schedule.Next(localAfter)
	if nextLocal.IsZero() {
		return nil, errors.New("unable to calculate next run from schedule")
	}

	nextUTC := nextLocal.UTC()
	return &nextUTC, nil
}
