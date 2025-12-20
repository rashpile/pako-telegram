// Package status provides system metrics collection.
package status

import (
	"context"
	"fmt"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
)

// Metrics holds system resource usage.
type Metrics struct {
	CPUPercent    float64
	MemoryUsed    uint64
	MemoryTotal   uint64
	MemoryPercent float64
	DiskUsed      uint64
	DiskTotal     uint64
	DiskPercent   float64
}

// Collector gathers system metrics.
type Collector interface {
	Collect(ctx context.Context) (*Metrics, error)
}

// GopsutilCollector uses gopsutil for metrics.
type GopsutilCollector struct {
	diskPath string
}

// NewGopsutilCollector creates a collector.
func NewGopsutilCollector() *GopsutilCollector {
	return &GopsutilCollector{
		diskPath: "/",
	}
}

// Collect gathers current system metrics.
func (c *GopsutilCollector) Collect(ctx context.Context) (*Metrics, error) {
	var m Metrics

	// CPU
	cpuPercent, err := cpu.PercentWithContext(ctx, 0, false)
	if err != nil {
		return nil, fmt.Errorf("get cpu: %w", err)
	}
	if len(cpuPercent) > 0 {
		m.CPUPercent = cpuPercent[0]
	}

	// Memory
	memInfo, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("get memory: %w", err)
	}
	m.MemoryUsed = memInfo.Used
	m.MemoryTotal = memInfo.Total
	m.MemoryPercent = memInfo.UsedPercent

	// Disk
	diskInfo, err := disk.UsageWithContext(ctx, c.diskPath)
	if err != nil {
		return nil, fmt.Errorf("get disk: %w", err)
	}
	m.DiskUsed = diskInfo.Used
	m.DiskTotal = diskInfo.Total
	m.DiskPercent = diskInfo.UsedPercent

	return &m, nil
}
