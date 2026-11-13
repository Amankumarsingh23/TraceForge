package storage

import (
	"context"
	"errors"
	"strings"
	"testing"
	"traceforge/internal/logs"
)

func TestLogWriterPreservesPhysicalRows(t *testing.T) {
	for _, kind := range []string{"success", "prepare", "append", "send"} {
		t.Run(kind, func(t *testing.T) {
			b := &batchStub{}
			c := &connStub{batch: b}
			switch kind {
			case "prepare":
				c.err = errors.New("secret")
			case "append":
				b.appendErr = errors.New("secret")
			case "send":
				b.sendErr = errors.New("secret")
			}
			e := (&Store{Conn: c}).InsertLogs(context.Background(), make([]logs.Row, 3))
			if (e == nil) != (kind == "success") {
				t.Fatal("wrong write outcome")
			}
			if e != nil && strings.Contains(e.Error(), "secret") {
				t.Fatal("driver detail leaked")
			}
			if kind == "success" && (b.appended != 3 || b.sent != 1) {
				t.Fatal("log rows deduplicated or not batched")
			}
			if kind != "prepare" && b.closed != 1 {
				t.Fatal("batch leaked")
			}
		})
	}
}
