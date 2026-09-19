package sqlite

import (
	"context"
	"strings"
	"testing"
)

func TestMatchTierAndConflicts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		left  identity
		right identity
		want  int
	}{
		{"handle", identity{handle: "h"}, identity{handle: "h"}, 6},
		{"checkout data", identity{checkout: "data"}, identity{checkout: "data"}, 5},
		{"pid and ip", identity{pid: "12", ip: "10.0.0.1"}, identity{pid: "12", ip: "10.0.0.1"}, 4},
		{"user and host", identity{user: "u", host: "h"}, identity{user: "u", host: "h"}, 3},
		{"host", identity{host: "h"}, identity{host: "h"}, 2},
		{"user", identity{user: "u"}, identity{user: "u"}, 1},
		{"placeholder", identity{handle: "N/A", user: "u"}, identity{handle: "N/A", user: "u"}, 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := matchTier(test.left, test.right); got != test.want {
				t.Fatalf("tier = %d, want %d", got, test.want)
			}
			if !compatible(test.left, test.right) {
				t.Fatal("matching identities are incompatible")
			}
		})
	}
	if compatible(identity{user: "left", host: "same"}, identity{user: "right", host: "same"}) {
		t.Fatal("conflicting populated users must disqualify a host match")
	}
}

func TestQuantitiesAmbiguityOpenAndOrphan(t *testing.T) {
	t.Parallel()
	store, err := Open(context.Background(), ":memory:", OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	input := strings.Join([]string{
		`2026-09-05T00:00:00Z (v) OUT: f u@h (2 licenses)`,
		`2026-09-05T00:10:00Z (v) OUT: f u@h`,
		`2026-09-05T00:20:00Z (v) IN: f u@h (2 licenses)`,
		`2026-09-05T00:30:00Z (v) IN: f u@h (2 licenses)`,
		`2026-09-05T00:40:00Z (v) OUT: f zero@h`,
		`2026-09-05T00:40:00Z (v) IN: f zero@h`,
		`2026-09-05T00:50:00Z (v) OUT: f open@h`,
	}, "\n")
	if _, err := store.Import(context.Background(), ImportRequest{Reader: strings.NewReader(input), Pool: "p", Stream: "s", SourceName: "a"}); err != nil {
		t.Fatal(err)
	}
	rows, err := store.db.Query(`SELECT state, quantity, ambiguous, end_ns-start_ns
		FROM sessions ORDER BY COALESCE(start_ns,end_ns), id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var closed, open, orphan, ambiguous, zero int
	for rows.Next() {
		var state string
		var quantity int
		var isAmbiguous bool
		var duration any
		if err := rows.Scan(&state, &quantity, &isAmbiguous, &duration); err != nil {
			t.Fatal(err)
		}
		switch state {
		case "closed":
			closed += quantity
		case "open":
			open += quantity
		case "orphan":
			orphan += quantity
		}
		if isAmbiguous {
			ambiguous += quantity
		}
		if value, ok := duration.(int64); ok && value == 0 {
			zero++
		}
	}
	if closed != 4 || open != 1 || orphan != 1 || ambiguous != 2 || zero != 1 {
		t.Fatalf("closed=%d open=%d orphan=%d ambiguous=%d zero=%d", closed, open, orphan, ambiguous, zero)
	}
}
