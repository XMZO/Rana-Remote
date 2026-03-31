package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rana-remote/rana-remote/internal/store"
)

type Runner struct {
	repo      store.Repository
	trigger   func(context.Context, store.Schedule) error
	pollEvery time.Duration
	stop      chan struct{}
}

func NewRunner(repo store.Repository, trigger func(context.Context, store.Schedule) error) *Runner {
	return &Runner{repo: repo, trigger: trigger, pollEvery: time.Minute, stop: make(chan struct{})}
}

func (r *Runner) Start(ctx context.Context) error {
	ticker := time.NewTicker(r.pollEvery)
	defer ticker.Stop()
	for {
		if err := r.tick(ctx, time.Now().UTC()); err != nil {
			log.Printf("scheduler tick failed: %v", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-r.stop:
			return nil
		case <-ticker.C:
		}
	}
}

func (r *Runner) Stop(context.Context) error {
	select {
	case <-r.stop:
	default:
		close(r.stop)
	}
	return nil
}

func (r *Runner) tick(ctx context.Context, now time.Time) error {
	schedules, err := r.repo.ListSchedules(ctx)
	if err != nil {
		return err
	}
	for _, sch := range schedules {
		if !sch.Enabled {
			continue
		}
		if sch.NextRunAt.IsZero() {
			next, err := NextRunAt(sch.CronExpr, now, sch.Timezone)
			if err != nil {
				log.Printf("compute next run failed %s: %v", sch.ID, err)
				continue
			}
			sch.NextRunAt = next
			if err := r.repo.UpdateSchedule(ctx, sch); err != nil {
				log.Printf("update schedule next run failed %s: %v", sch.ID, err)
			}
			continue
		}

		if sch.NextRunAt.After(now) {
			continue
		}

		if sch.MisfirePolicy == "skip" && now.Sub(sch.NextRunAt) > r.pollEvery {
			next, err := NextRunAt(sch.CronExpr, now, sch.Timezone)
			if err == nil {
				sch.NextRunAt = next
				_ = r.repo.UpdateSchedule(ctx, sch)
			}
			continue
		}

		if r.trigger != nil {
			if err := r.trigger(ctx, sch); err != nil {
				log.Printf("trigger schedule %s failed: %v", sch.ID, err)
			}
		}
		sch.LastRunAt = now
		next, err := NextRunAt(sch.CronExpr, now, sch.Timezone)
		if err != nil {
			log.Printf("compute next run for %s failed: %v", sch.ID, err)
			continue
		}
		sch.NextRunAt = next
		if err := r.repo.UpdateSchedule(ctx, sch); err != nil {
			log.Printf("update schedule after run failed %s: %v", sch.ID, err)
		}
	}
	return nil
}

// NextRunAt computes the next run time for supported expressions.
func NextRunAt(expr string, from time.Time, timezone string) (time.Time, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return time.Time{}, errors.New("cron expression is required")
	}
	loc := time.UTC
	if strings.TrimSpace(timezone) != "" {
		loaded, err := time.LoadLocation(timezone)
		if err != nil {
			return time.Time{}, fmt.Errorf("load timezone: %w", err)
		}
		loc = loaded
	}
	base := from.In(loc)

	if strings.HasPrefix(expr, "@every ") {
		d, err := time.ParseDuration(strings.TrimSpace(strings.TrimPrefix(expr, "@every ")))
		if err != nil {
			return time.Time{}, fmt.Errorf("parse @every duration: %w", err)
		}
		if d <= 0 {
			return time.Time{}, errors.New("@every duration must be > 0")
		}
		return base.Add(d).UTC(), nil
	}

	parts := strings.Fields(expr)
	if len(parts) != 5 {
		return time.Time{}, errors.New("cron expression must contain 5 fields")
	}

	monthSet, err := parseCronField(parts[3], 1, 12, monthNames)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid month field: %w", err)
	}
	daySet, err := parseCronField(parts[2], 1, 31, nil)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid day-of-month field: %w", err)
	}
	hourSet, err := parseCronField(parts[1], 0, 23, nil)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid hour field: %w", err)
	}
	minuteSet, err := parseCronField(parts[0], 0, 59, nil)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid minute field: %w", err)
	}
	dowSet, err := parseCronField(parts[4], 0, 7, dowNames)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid day-of-week field: %w", err)
	}
	if dowSet[7] {
		dowSet[0] = true
		delete(dowSet, 7)
	}

	next := base.Truncate(time.Minute).Add(time.Minute)
	for i := 0; i < 366*24*60; i++ {
		if matchesCron(next, minuteSet, hourSet, daySet, monthSet, dowSet) {
			return next.UTC(), nil
		}
		next = next.Add(time.Minute)
	}
	return time.Time{}, errors.New("unable to compute next run")
}

var monthNames = map[string]int{
	"jan": 1,
	"feb": 2,
	"mar": 3,
	"apr": 4,
	"may": 5,
	"jun": 6,
	"jul": 7,
	"aug": 8,
	"sep": 9,
	"oct": 10,
	"nov": 11,
	"dec": 12,
}

var dowNames = map[string]int{
	"sun": 0,
	"mon": 1,
	"tue": 2,
	"wed": 3,
	"thu": 4,
	"fri": 5,
	"sat": 6,
}

func matchesCron(t time.Time, minutes, hours, dom, months, dow map[int]bool) bool {
	if !minutes[t.Minute()] || !hours[t.Hour()] || !months[int(t.Month())] || !dow[int(t.Weekday())] {
		return false
	}
	return dom[t.Day()]
}

func parseCronField(raw string, min, max int, names map[string]int) (map[int]bool, error) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return nil, errors.New("empty field")
	}
	set := make(map[int]bool)
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, errors.New("empty list item")
		}
		step := 1
		base := part
		if strings.Contains(part, "/") {
			pieces := strings.Split(part, "/")
			if len(pieces) != 2 {
				return nil, fmt.Errorf("invalid step syntax %q", part)
			}
			base = strings.TrimSpace(pieces[0])
			parsedStep, err := strconv.Atoi(strings.TrimSpace(pieces[1]))
			if err != nil || parsedStep <= 0 {
				return nil, fmt.Errorf("invalid step %q", part)
			}
			step = parsedStep
		}

		start, end, err := parseCronRange(base, min, max, names)
		if err != nil {
			return nil, err
		}
		for v := start; v <= end; v += step {
			set[v] = true
		}
	}
	if len(set) == 0 {
		return nil, errors.New("field resolves to empty set")
	}
	return set, nil
}

func parseCronRange(raw string, min, max int, names map[string]int) (int, int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "*" || raw == "" {
		return min, max, nil
	}
	if strings.Contains(raw, "-") {
		pieces := strings.Split(raw, "-")
		if len(pieces) != 2 {
			return 0, 0, fmt.Errorf("invalid range %q", raw)
		}
		start, err := parseCronValue(strings.TrimSpace(pieces[0]), min, max, names)
		if err != nil {
			return 0, 0, err
		}
		end, err := parseCronValue(strings.TrimSpace(pieces[1]), min, max, names)
		if err != nil {
			return 0, 0, err
		}
		if end < start {
			return 0, 0, fmt.Errorf("range end before start %q", raw)
		}
		return start, end, nil
	}
	value, err := parseCronValue(raw, min, max, names)
	if err != nil {
		return 0, 0, err
	}
	return value, value, nil
}

func parseCronValue(raw string, min, max int, names map[string]int) (int, error) {
	if names != nil {
		if v, ok := names[strings.ToLower(raw)]; ok {
			return v, nil
		}
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid value %q", raw)
	}
	if v < min || v > max {
		return 0, fmt.Errorf("value %d out of range [%d,%d]", v, min, max)
	}
	return v, nil
}

func sortedValues(set map[int]bool) []int {
	out := make([]int, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Ints(out)
	return out
}
