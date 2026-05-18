package urlutil

import "strings"

// slashPlaceholder replaces %2F (percent-encoded slash) in URL paths before
// Echo route matching. Go's net/http decodes %2F → / before routing, so a
// settings name like "org/team-slug" sent as "org%2Fteam-slug" would be split
// into two path segments, causing a 404 on routes like /settings/:name/sync/push.
const slashPlaceholder = "\x01"

// RewriteEncodedSlashes replaces %2F occurrences in rawPath with a placeholder
// and returns the rewritten path. The second return value is true when a
// substitution was made. If rawPath contains no encoded slashes the first
// return value equals rawPath and ok is false.
func RewriteEncodedSlashes(rawPath string) (rewritten string, ok bool) {
	normalized := strings.ReplaceAll(rawPath, "%2f", "%2F")
	if !strings.Contains(normalized, "%2F") {
		return rawPath, false
	}
	return strings.ReplaceAll(normalized, "%2F", slashPlaceholder), true
}

// DecodeSlashParam restores the placeholder back to / in a path parameter
// value. Use this instead of c.Param() for any route parameter that may
// contain a slash (e.g. settings name = "org/team-slug").
func DecodeSlashParam(param string) string {
	return strings.ReplaceAll(param, slashPlaceholder, "/")
}
