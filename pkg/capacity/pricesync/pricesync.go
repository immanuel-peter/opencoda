package pricesync

import (
	"context"
	"encoding/csv"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const catalogURL = "https://raw.githubusercontent.com/skypilot-org/skypilot-catalog/master/v1/aws/vms.csv"

// Table holds hourly USD prices keyed by instance type.
type Table struct {
	mu     sync.RWMutex
	prices map[string]float64
}

func NewTable() *Table {
	return &Table{prices: make(map[string]float64)}
}

func (t *Table) Get(instanceType string) float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.prices[instanceType]
}

func (t *Table) Set(instanceType string, hourly float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.prices[instanceType] = hourly
}

// SyncJob periodically downloads skypilot-catalog CSV.
type SyncJob struct {
	Table   *Table
	URL     string
	Period  time.Duration
}

func NewSyncJob() *SyncJob {
	return &SyncJob{
		Table:  NewTable(),
		URL:    catalogURL,
		Period: 6 * time.Hour,
	}
}

func (j *SyncJob) Start(ctx context.Context) error {
	j.syncOnce(ctx)
	ticker := time.NewTicker(j.Period)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			j.syncOnce(ctx)
		}
	}
}

func (j *SyncJob) syncOnce(ctx context.Context) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, j.URL, nil)
	if err != nil {
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	r := csv.NewReader(resp.Body)
	header, err := r.Read()
	if err != nil {
		return
	}
	typeIdx, priceIdx := -1, -1
	for i, h := range header {
		switch h {
		case "InstanceType":
			typeIdx = i
		case "Price":
			priceIdx = i
		}
	}
	if typeIdx < 0 || priceIdx < 0 {
		return
	}
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil || len(row) <= priceIdx {
			continue
		}
		price, err := strconv.ParseFloat(row[priceIdx], 64)
		if err != nil {
			continue
		}
		j.Table.Set(row[typeIdx], price)
	}
}
