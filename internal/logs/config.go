package logs

import (
	"errors"

	"time"
)

type Config struct {
	QueueRequests   int           `json:"queue_requests"`
	InFlight        int           `json:"in_flight"`
	PendingRequests int           `json:"pending_requests"`
	BufferedRecords int           `json:"buffered_records"`
	BufferedBytes   int64         `json:"buffered_bytes"`
	BatchTarget     int           `json:"batch_target"`
	Workers         int           `json:"workers"`
	BatchWait       time.Duration `json:"batch_wait_ns"`
	AdmissionWait   time.Duration `json:"admission_wait_ns"`
	WriteTimeout    time.Duration `json:"write_timeout_ns"`
	ExportTimeout   time.Duration `json:"export_timeout_ns"`
}

func DefaultConfig() Config {
	return Config{QueueRequests: 32, InFlight: 16, PendingRequests: 64, BufferedRecords: 8192, BufferedBytes: 64 << 20, BatchTarget: 256, Workers: 1, BatchWait: 50 * time.Millisecond, AdmissionWait: 50 * time.Millisecond, WriteTimeout: 3 * time.Second, ExportTimeout: 5 * time.Second}
}
func (c Config) Validate() error {
	if c.QueueRequests < 1 || c.QueueRequests > 1024 || c.InFlight < 1 || c.InFlight > 256 || c.PendingRequests < c.QueueRequests || c.PendingRequests > 1024 || c.BufferedRecords < 1 || c.BufferedRecords > 100000 || c.BufferedBytes < 1 || c.BufferedBytes > 512<<20 || c.BatchTarget < 1 || c.BatchTarget > 10000 || c.Workers < 1 || c.Workers > 4 || c.BatchWait < 0 || c.BatchWait > time.Second || c.AdmissionWait <= 0 || c.AdmissionWait > time.Second || c.WriteTimeout <= 0 || c.WriteTimeout > 5*time.Second || c.ExportTimeout <= 0 || c.ExportTimeout > 10*time.Second {
		return errors.New("ingestion limits out of range")
	}
	return nil
}
