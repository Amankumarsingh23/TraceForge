package storage

import (
	"context"
	"errors"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"testing"
	"traceforge/internal/ingest"
)

// Embed official interfaces so tests override only the writer's narrow surface.
type batchStub struct {
	driver.Batch
	appended, sent, closed int
	appendErr, sendErr     error
}

func (b *batchStub) Append(...any) error { b.appended++; return b.appendErr }
func (b *batchStub) Send() error         { b.sent++; return b.sendErr }
func (b *batchStub) Close() error        { b.closed++; return nil }

type connStub struct {
	driver.Conn
	batch    *batchStub
	prepared int
	err      error
}

func (c *connStub) PrepareBatch(context.Context, string, ...driver.PrepareBatchOption) (driver.Batch, error) {
	c.prepared++
	return c.batch, c.err
}

func TestRequestBatchAndCleanup(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		prepare, append, send bool
	}{{"success", false, false, false}, {"prepare_failure", true, false, false}, {"append_failure", false, true, false}, {"send_failure", false, false, true}} {
		t.Run(tc.name, func(t *testing.T) {
			b := &batchStub{}
			c := &connStub{batch: b}
			if tc.prepare {
				c.err = errors.New("private driver details")
			}
			if tc.append {
				b.appendErr = errors.New("private driver details")
			}
			if tc.send {
				b.sendErr = errors.New("private driver details")
			}
			e := (&Store{Conn: c}).Insert(context.Background(), make([]ingest.Row, 3))
			if (e != nil) != (tc.prepare || tc.append || tc.send) {
				t.Fatal("unexpected write outcome", e)
			}
			if c.prepared != 1 {
				t.Fatal("not one request batch")
			}
			if !tc.prepare && b.closed != 1 {
				t.Fatal("batch not cleaned up")
			}
			if !tc.prepare && !tc.append && (b.sent != 1 || b.appended != 3) {
				t.Fatal("not one multi-row send")
			}
			if tc.append && b.sent != 0 {
				t.Fatal("sent failed batch")
			}
		})
	}
}
func TestEmptyAndCanceledBatch(t *testing.T) {
	b := &batchStub{}
	c := &connStub{batch: b}
	s := &Store{Conn: c}
	if e := s.Insert(context.Background(), nil); e != nil || c.prepared != 0 {
		t.Fatal("empty batch touched database")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := s.Insert(ctx, make([]ingest.Row, 1)); e != context.Canceled || b.sent != 0 || b.closed != 1 {
		t.Fatal("canceled batch not cleaned up", e)
	}
}
