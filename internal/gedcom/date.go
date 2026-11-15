package gedcom

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

var yearRe = regexp.MustCompile(`(?:^|[^0-9])(\d{3,4})(?:$|[^0-9])`)

// ParseYear extracts the first year (3-4 digit number) in a GEDCOM-style
// date value like "ABT 1847", "BET 1900 AND 1905", "JUL 1876". Returns 0
// when no year is present. Also used by the web forms (free-text dates).
func ParseYear(raw string) int64 {
	return yearFromString(raw)
}
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

// FormatDate converts a free-text or ISO date into a consistent, human-readable
// form. ISO calendar dates ("2024-03-15") become "March 15, 2024"; GEDCOM-style
// month abbreviations are expanded ("MAR 1847" -> "March 1847"); values that
// cannot be parsed are returned unchanged so GEDCOM qualifiers (ABT, BET ... AND)
// and partial dates keep their original meaning.
func FormatDate(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	layouts := []string{
		"2006-01-02", "2006-01", "2006",
		"2 January 2006", "2 Jan 2006",
		"January 2, 2006", "Jan 2, 2006",
		"January 2006", "Jan 2006",
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, raw); err == nil {
			if !strings.EqualFold(strings.TrimSpace(raw), strings.TrimSpace(t.Format(l))) {
				continue
			}
			return formatParsedDate(t, l)
		}
	}

	upper := strings.ToUpper(raw)
	for _, prefix := range []string{"ABT ", "ABOUT ", "BEF ", "AFT ", "EST ", "CAL ", "CIRCA "} {
		if strings.HasPrefix(upper, prefix) {
			rest := strings.TrimSpace(raw[len(prefix):])
			if formatted := FormatDate(rest); formatted != rest {
				return raw[:len(prefix)] + formatted
			}
			return raw
		}
	}

	if strings.HasPrefix(upper, "BET ") {
		if idx := strings.Index(upper, " AND "); idx >= 0 {
			left := strings.TrimSpace(raw[4:idx])
			right := strings.TrimSpace(raw[idx+5:])
			lf := FormatDate(left)
			rf := FormatDate(right)
			if lf != left || rf != right {
				return "between " + lf + " and " + rf
			}
			return raw
		}
	}

	return raw
}

func formatParsedDate(t time.Time, layout string) string {
	dayLayouts := map[string]bool{
		"2006-01-02":      true,
		"2 January 2006":  true,
		"2 Jan 2006":      true,
		"January 2, 2006": true,
		"Jan 2, 2006":     true,
	}
	monthLayouts := map[string]bool{
		"2006-01":         true,
		"2006-01-02":      true,
		"2 January 2006":  true,
		"2 Jan 2006":      true,
		"January 2, 2006": true,
		"Jan 2, 2006":     true,
		"January 2006":    true,
		"Jan 2006":        true,
	}
	switch {
	case dayLayouts[layout]:
		return t.Format("January 2, 2006")
	case monthLayouts[layout]:
		return t.Format("January 2006")
	default:
		return t.Format("2006")
	}
}
