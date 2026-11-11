package fleet

import (
	"testing"
	"time"
)

func TestForecastFill(t *testing.T) {
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	var rising []MetricPoint
	for i := range 8 {
		rising = append(rising, MetricPoint{
			At: start.Add(time.Duration(i) * 24 * time.Hour), DiskPercent: 40 + float64(i)*5,
		})
	}
	days, ok := forecastFill(rising)
	if !ok || days < 3 || days > 8 {
		t.Fatalf("rising disk: %d days, ok=%v", days, ok)
	}
	var flat []MetricPoint
	for i := range 8 {
		flat = append(flat, MetricPoint{At: start.Add(time.Duration(i) * 24 * time.Hour), DiskPercent: 40})
	}
	if _, ok := forecastFill(flat); ok {
		t.Fatal("a flat disk must not produce a forecast")
	}
	var falling []MetricPoint
	for i := range 8 {
		falling = append(falling, MetricPoint{At: start.Add(time.Duration(i) * 24 * time.Hour), DiskPercent: 80 - float64(i)*2})
	}
	if _, ok := forecastFill(falling); ok {
		t.Fatal("a shrinking disk must not produce a forecast")
	}
	short := []MetricPoint{
		{At: start, DiskPercent: 10},
		{At: start.Add(time.Hour), DiskPercent: 20},
	}
	if _, ok := forecastFill(short); ok {
		t.Fatal("less than a day of samples must not produce a forecast")
	}
}
