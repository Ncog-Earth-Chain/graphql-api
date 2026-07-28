package resolvers

// trimProbePage turns a store page that carries one row past the end into the page the
// client asked for, plus the two boundary flags.
//
// It exists because three lists -- logs, ddbOperations and ddbContracts -- derived
// hasNextPage from "the page came back short" and hardcoded hasPreviousPage to false. Both
// halves were wrong, and neither could be right without a probe row:
//
//   - A short page does not mean the last page. An EXACTLY-full final page looks identical
//     to a full page with more behind it, so the last page always claimed a next page that
//     is empty. Not a rare edge either: the API's per-request cap (listMaxEdgesPerRequest)
//     is below the store's own maximum, so every full page is exactly full.
//   - hasPreviousPage was the literal constant false, so no page ever admitted to having
//     anything before it -- including a page reached by following a cursor.
//
// repository/adapt.go records having fixed exactly this pair for transactions; this is the
// same correction for the lists that were left behind.
//
// The direction matters. `more` describes the end the scan is travelling TOWARD, and a
// negative count travels the other way, so for a backward page it bounds the PREVIOUS side
// while the absent cursor bounds the next one.
func trimProbePage[T any](rows []T, asked int32, hasCursor bool) (page []T, hasNext bool, hasPrev bool) {
	limit := int(absCount(asked))
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}

	if asked >= 0 {
		return rows, more, hasCursor
	}
	return rows, hasCursor, more
}
