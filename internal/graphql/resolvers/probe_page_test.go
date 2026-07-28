package resolvers

import "testing"

// TestTrimProbePage covers the two failures the old short-page inference could not avoid.
//
// The important row is "exactly full, nothing behind": the old code compared the page length
// against the requested count, so a final page that happened to fill exactly was
// indistinguishable from a full page with more to come, and always claimed a next page that
// is empty. Every full page is exactly full here, because the API's per-request cap is below
// the store's own maximum -- so this was the common case, not an edge.
func TestTrimProbePage(t *testing.T) {
	cases := []struct {
		name      string
		rows      int
		asked     int32
		hasCursor bool

		wantLen  int
		wantNext bool
		wantPrev bool
	}{
		{
			name: "exactly full, nothing behind: no next page",
			// The store returned no probe row, so the page really is the last one.
			rows: 25, asked: 25, hasCursor: false,
			wantLen: 25, wantNext: false, wantPrev: false,
		},
		{
			name: "exactly full, more behind: probe row detected and trimmed",
			rows: 26, asked: 25, hasCursor: false,
			wantLen: 25, wantNext: true, wantPrev: false,
		},
		{
			name: "short page: no next page",
			rows: 7, asked: 25, hasCursor: false,
			wantLen: 7, wantNext: false, wantPrev: false,
		},
		{
			name: "reached by a cursor: there IS a previous page",
			// hasPreviousPage used to be the constant false, so this never reported true.
			rows: 26, asked: 25, hasCursor: true,
			wantLen: 25, wantNext: true, wantPrev: true,
		},
		{
			name: "empty page",
			rows: 0, asked: 25, hasCursor: false,
			wantLen: 0, wantNext: false, wantPrev: false,
		},
		{
			// A negative count travels the other way, so the probe bounds the PREVIOUS side
			// and the absent cursor bounds the next one.
			name: "backward page with more behind it",
			rows: 26, asked: -25, hasCursor: false,
			wantLen: 25, wantNext: false, wantPrev: true,
		},
		{
			name: "backward page reached by a cursor",
			rows: 26, asked: -25, hasCursor: true,
			wantLen: 25, wantNext: true, wantPrev: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rows := make([]int, c.rows)
			for i := range rows {
				rows[i] = i
			}

			page, hasNext, hasPrev := trimProbePage(rows, c.asked, c.hasCursor)

			if len(page) != c.wantLen {
				t.Errorf("page length = %d, want %d (the probe row must not reach the client)", len(page), c.wantLen)
			}
			if hasNext != c.wantNext {
				t.Errorf("hasNext = %v, want %v", hasNext, c.wantNext)
			}
			if hasPrev != c.wantPrev {
				t.Errorf("hasPrev = %v, want %v", hasPrev, c.wantPrev)
			}

			// Trimming must take the probe off the END; the page itself keeps store order.
			for i := range page {
				if page[i] != i {
					t.Fatalf("page[%d] = %d, want %d -- order changed by trimming", i, page[i], i)
				}
			}
		})
	}
}
