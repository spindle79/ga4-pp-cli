// Copyright 2026 spindle79. Licensed under Apache-2.0. See LICENSE.

package cli

import "testing"

func TestGuardReadOnlySQL(t *testing.T) {
	cases := []struct {
		name    string
		q       string
		wantErr bool
	}{
		{"select", "SELECT 1", false},
		{"select lower", "select * from pages_daily", false},
		{"with", "WITH t AS (SELECT 1) SELECT * FROM t", false},
		{"explain", "EXPLAIN QUERY PLAN SELECT 1", false},
		{"pragma", "PRAGMA user_version", false},
		{"leading comment", "-- comment\nSELECT 1", false},
		{"insert", "INSERT INTO pages_daily VALUES (1)", true},
		{"update", "UPDATE pages_daily SET sessions = 0", true},
		{"delete", "DELETE FROM pages_daily", true},
		{"drop", "DROP TABLE pages_daily", true},
		{"empty", "", true},
		{"whitespace", "   ", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := guardReadOnlySQL(c.q)
			if (err != nil) != c.wantErr {
				t.Fatalf("guardReadOnlySQL(%q) err=%v, wantErr=%v", c.q, err, c.wantErr)
			}
		})
	}
}
