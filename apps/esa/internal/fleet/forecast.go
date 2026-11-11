package fleet

import (
	"context"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// DiskForecast estimates how many days until the fullest disk reaches 100
// percent. ok is false when there is less than a day of samples, the disk
// is not filling, or the estimate is longer than 180 days.
func (s *Service) DiskForecast(ctx context.Context, a Actor, serverID int64) (int, bool, error) {
	if err := a.can(RoleViewer, ScopeRead); err != nil {
		return 0, false, err
	}
	if _, _, err := s.loadVisibleServer(ctx, a, serverID); err != nil {
		return 0, false, err
	}
	since := s.now().Add(-14 * 24 * time.Hour)
	var rows []store.MetricHourly
	if err := s.db.WithContext(ctx).Where("server_id = ? AND hour >= ?", serverID, since).Order("hour").Find(&rows).Error; err != nil {
		return 0, false, err
	}
	points := make([]MetricPoint, 0, len(rows))
	for _, r := range rows {
		points = append(points, MetricPoint{At: r.Hour, DiskPercent: r.DiskPercent})
	}
	if len(points) < 6 {
		var raw []store.MetricSample
		rawSince := s.now().Add(-48 * time.Hour)
		if err := s.db.WithContext(ctx).Where("server_id = ? AND at >= ?", serverID, rawSince).Order("at").Find(&raw).Error; err != nil {
			return 0, false, err
		}
		points = points[:0]
		for _, r := range raw {
			points = append(points, MetricPoint{At: r.At, DiskPercent: r.DiskPercent})
		}
	}
	days, ok := forecastFill(points)
	return days, ok, nil
}

// forecastFill fits a straight line to disk percent. The slope has to be at
// least 0.2 points a day, so a flat week does not produce a number.
func forecastFill(points []MetricPoint) (int, bool) {
	if len(points) < 6 {
		return 0, false
	}
	t0 := points[0].At
	if points[len(points)-1].At.Sub(t0) < 24*time.Hour {
		return 0, false
	}
	var n, sumX, sumY, sumXX, sumXY float64
	for _, p := range points {
		x := p.At.Sub(t0).Hours()
		y := p.DiskPercent
		n++
		sumX += x
		sumY += y
		sumXX += x * x
		sumXY += x * y
	}
	denom := n*sumXX - sumX*sumX
	if denom == 0 {
		return 0, false
	}
	perDay := (n*sumXY - sumX*sumY) / denom * 24
	if perDay < 0.2 {
		return 0, false
	}
	last := points[len(points)-1].DiskPercent
	if last >= 98 {
		return 1, true
	}
	days := (100 - last) / perDay
	if days > 180 {
		return 0, false
	}
	d := int(days + 0.5)
	if d < 1 {
		d = 1
	}
	return d, true
}
