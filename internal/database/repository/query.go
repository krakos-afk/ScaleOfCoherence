package repository

import "strings"

// escapeLike escapes LIKE wildcards so user search text matches literally.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// dateSortKeys sort newest-first by default.
var dateSortKeys = map[string]bool{"created_at": true, "updated_at": true}

// orderBy resolves sortKey against the whitelist (falling back to fallbackKey)
// and returns "<expr> <DIR>". An empty order defaults to descending for
// created_at/updated_at and ascending for everything else.
func orderBy(sorts map[string]string, fallbackKey, sortKey, order string) string {
	expr, ok := sorts[sortKey]
	if !ok {
		sortKey = fallbackKey
		expr = sorts[fallbackKey]
	}
	dir := "ASC"
	if order == "desc" || (order != "asc" && dateSortKeys[sortKey]) {
		dir = "DESC"
	}
	return expr + " " + dir
}
