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

func TestConsumeOpenAllocation(t *testing.T) {
	t.Parallel()
	first := &openAllocation{eventID: 1, quantity: 1}
	partial := &openAllocation{eventID: 2, quantity: 3}
	last := &openAllocation{eventID: 3, quantity: 1}
	storage := []*openAllocation{first, partial, last}

	open := consumeOpenAllocation(storage, 1, 1)
	if len(open) != 3 || open[1] != partial || open[1].quantity != 2 {
		t.Fatalf("partial allocation moved or changed incorrectly: %+v", open)
	}

	open = consumeOpenAllocation(open, 1, 2)
	if len(open) != 2 || open[0] != first || open[1] != last {
		t.Fatalf("stable removal = %+v", open)
	}
	if storage[2] != nil {
		t.Fatal("removed tail pointer was retained")
	}
}

func TestConsumedAllocationRetentionIsBounded(t *testing.T) {
	t.Parallel()
	var open []*openAllocation
	for i := 0; i < 100_000; i++ {
		open = append(open, &openAllocation{eventID: int64(i + 1), quantity: 1})
		open = consumeOpenAllocation(open, 0, 1)
		if len(open) != 0 {
			t.Fatalf("iteration %d retained %d allocations", i, len(open))
		}
	}
}

func TestSessionMatchOrderingSurvivesCompaction(t *testing.T) {
	t.Parallel()
	store, err := Open(context.Background(), ":memory:", OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	input := strings.Join([]string{
		`2026-09-05T00:00:00Z (v) OUT: f alice@host`,
		`2026-09-05T00:01:00Z (v) OUT: f alice@host [job]`,
		`2026-09-05T00:02:00Z (v) IN: f alice@host [job]`,
		`2026-09-05T00:03:00Z (v) OUT: f bob@host (2 licenses)`,
		`2026-09-05T00:04:00Z (v) OUT: f bob@host`,
		`2026-09-05T00:05:00Z (v) IN: f bob@host (3 licenses)`,
		`2026-09-05T00:06:00Z (v) IN: f nobody@elsewhere`,
	}, "\n")
	if _, err := store.Import(context.Background(), ImportRequest{
		Reader: strings.NewReader(input), Pool: "p", Stream: "s", SourceName: "ordering.log",
	}); err != nil {
		t.Fatal(err)
	}

	rows, err := store.db.Query(`SELECT s.state,s.quantity,s.match_tier,s.ambiguous,e.source_line
		FROM sessions s LEFT JOIN activity_events e ON e.id=s.opening_event_id
		ORDER BY s.id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type session struct {
		state       string
		quantity    int
		tier        int
		ambiguous   bool
		openingLine *int
	}
	var sessions []session
	for rows.Next() {
		var got session
		if err := rows.Scan(&got.state, &got.quantity, &got.tier, &got.ambiguous, &got.openingLine); err != nil {
			t.Fatal(err)
		}
		sessions = append(sessions, got)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 5 {
		t.Fatalf("sessions = %+v", sessions)
	}
	if sessions[0].state != "closed" || sessions[0].tier != 5 || sessions[0].openingLine == nil || *sessions[0].openingLine != 2 {
		t.Fatalf("higher-tier session = %+v", sessions[0])
	}
	for i, line := range []int{4, 5} {
		got := sessions[i+1]
		if got.state != "closed" || !got.ambiguous || got.openingLine == nil || *got.openingLine != line {
			t.Fatalf("ambiguous session %d = %+v", i, got)
		}
	}
	if sessions[3].state != "orphan" || sessions[3].quantity != 1 || sessions[3].openingLine != nil {
		t.Fatalf("orphan session = %+v", sessions[3])
	}
	if sessions[4].state != "open" || sessions[4].openingLine == nil || *sessions[4].openingLine != 1 {
		t.Fatalf("open session = %+v", sessions[4])
	}
}
