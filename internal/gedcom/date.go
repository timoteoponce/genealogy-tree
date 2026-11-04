package gedcom

import (
	"regexp"
	"strconv"
)

var yearRe = regexp.MustCompile(`(?:^|[^0-9])(\d{3,4})(?:$|[^0-9])`)

// yearFromString extracts the first year (3-4 digit number) in a GEDCOM
// date value like "ABT 1847", "BET 1900 AND 1905", "JUL 1876". Returns 0
// when no year is present.
func yearFromString(raw string) int64 {
	m := yearRe.FindStringSubmatch(raw)
	if m == nil {
		return 0
	}
	v, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0
	}
	return v
}
