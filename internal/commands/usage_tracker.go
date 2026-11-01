package commands

import (
	"math"
	"sort"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/history"
)

// CommandUsageTracker scores commands by frequency with a recency half-life
// so the slash menu surfaces what the user actually reaches for.
type CommandUsageTracker struct{}

func (t CommandUsageTracker) score(entry history.CommandUsage, found bool, now time.Time) float64 {
	if !found {
		return 0
	}
	daysSince := math.Max(0, float64(now.UnixMilli()-entry.LastUsedAt)) / float64(MillisecondsPerDay)
	recency := math.Pow(0.5, daysSince/float64(CommandRecencyHalfLifeDays))
	return float64(entry.UsageCount) * math.Max(recency, CommandMinRecencyWeight)
}

func (t CommandUsageTracker) GetScore(name string) float64 {
	usage, found := history.GetCommandUsage()[name]
	return t.score(usage, found, time.Now())
}

func (t CommandUsageTracker) GetRecentlyUsed(limit int) []string {
	if limit <= 0 {
		return nil
	}
	now := time.Now()
	type scored struct {
		name  string
		score float64
	}
	var entries []scored
	for name, usage := range history.GetCommandUsage() {
		if s := t.score(usage, true, now); s > 0 {
			entries = append(entries, scored{name: name, score: s})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].score > entries[j].score })
	out := make([]string, 0, limit)
	for i, entry := range entries {
		if i >= limit {
			break
		}
		out = append(out, entry.name)
	}
	return out
}
