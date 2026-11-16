package storage

import "testing"

func TestIsolatedDatabaseRewrite(t *testing.T) {
	for _, name := range []string{"other", "tf_bench_x;DROP", "traceforge.evil", "TF_BENCH_x"} {
		if validateDatabase(name) == nil {
			t.Fatal(name)
		}
	}
	if validateDatabase("tf_bench_s13") == nil {
		s := Store{Database: "tf_bench_s13"}
		if s.sql("SELECT * FROM traceforge.spans WHERE trace_id=?") != "SELECT * FROM tf_bench_s13.spans WHERE trace_id=?" {
			t.Fatal("wrong database")
		}
	} else {
		t.Fatal("safe database rejected")
	}
	s := Store{}
	if s.sql("traceforge.logs") != "traceforge.logs" {
		t.Fatal("default changed")
	}
}
