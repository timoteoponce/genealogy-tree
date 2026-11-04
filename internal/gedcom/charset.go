// Package gedcom imports GEDCOM 5.5.1 (incl. MyHeritage exports) into the
// database. Pipeline: bytes → charset normalize → parse (elliotchance/gedcom)
// → map → replace-tree transaction → report.
package gedcom

import (
	"bytes"
	"fmt"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

// normalizeCharset transcodes raw GEDCOM bytes to UTF-8, reading the declared
// HEAD.CHAR. Returns the UTF-8 payload plus human-readable warnings.
func normalizeCharset(raw []byte) (out []byte, warnings []string) {
	// UTF-16 BOM detection comes first: the declared header text itself is
	// doubled-byte and cannot be scanned until decoded.
	if len(raw) >= 2 && ((raw[0] == 0xFF && raw[1] == 0xFE) || (raw[0] == 0xFE && raw[1] == 0xFF)) {
		return decodeUTF16(raw, &warnings), warnings
	}

	declared := declaredCharset(raw)

	switch canon(declared) {
	case "UNICODE", "UTF-16":
		out = decodeUTF16(raw, &warnings)
	case "UTF-8":
		out = raw
		if !utf8.Valid(out) {
			warnings = append(warnings, "charset: claims UTF-8 but is not valid UTF-8; falling back to Windows-1252")
			out, _ = charmap.Windows1252.NewDecoder().Bytes(raw)
		}
	case "ANSI", "WINDOWS-1252", "CP1252", "1252", "IBMPC", "IBM-WINDOWS":
		out, _ = charmap.Windows1252.NewDecoder().Bytes(raw)
	case "LATIN1", "ISO-8859-1", "ANSI-8859-1":
		out, _ = charmap.ISO8859_1.NewDecoder().Bytes(raw)
	case "ANSEL":
		out = anselToUTF8(raw, &warnings)
	default:
		// Unknown declaration: trust the bytes. If they are valid UTF-8, use
		// them; otherwise assume Windows-1252 (the usual mislabeled default).
		if utf8.Valid(bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})) {
			out = raw
			if declared != "" && canon(declared) != "UTF-8" {
				warnings = append(warnings,
					fmt.Sprintf("charset: unknown CHARSET %q but bytes look like UTF-8", declared))
			}
		} else {
			out, _ = charmap.Windows1252.NewDecoder().Bytes(raw)
			warnings = append(warnings,
				fmt.Sprintf("charset: unknown CHARSET %q, treated as Windows-1252", declared))
		}
	}

	// Strip a UTF-8 BOM if the decoder left one.
	out = bytes.TrimPrefix(out, []byte{0xEF, 0xBB, 0xBF})
	return out, warnings
}

// declaredCharset scans the head of the file for the "1 CHAR <value>" line.
func declaredCharset(raw []byte) string {
	head := raw
	if len(head) > 64*1024 {
		head = head[:64*1024]
	}
	for _, line := range strings.Split(string(head), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "1 CHAR") {
			return strings.TrimSpace(strings.TrimPrefix(line, "1 CHAR"))
		}
	}
	return ""
}

func canon(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}

// decodeUTF16 handles the "UNICODE"/"UTF-16" declarations via BOM sniffing
// (little-endian exports dominate; without BOM we assume little-endian).
func decodeUTF16(raw []byte, warnings *[]string) []byte {
	be := false
	switch {
	case len(raw) >= 2 && raw[0] == 0xFF && raw[1] == 0xFE:
		raw = raw[2:]
	case len(raw) >= 2 && raw[0] == 0xFE && raw[1] == 0xFF:
		be = true
		raw = raw[2:]
	default:
		*warnings = append(*warnings, "charset: UTF-16 without BOM; assuming little-endian")
	}

	u16 := make([]uint16, 0, len(raw)/2)
	for i := 0; i+1 < len(raw); i += 2 {
		if be {
			u16 = append(u16, uint16(raw[i])<<8|uint16(raw[i+1]))
		} else {
			u16 = append(u16, uint16(raw[i+1])<<8|uint16(raw[i]))
		}
	}
	return []byte(string(utf16.Decode(u16)))
}

// --- ANSEL (ANSI/NISO Z39.47 / GEDCOM 5.5 Appendix D) ---------------------
//
// Single-byte high codes map as follows (indexes are byte - 0xA0):
//   - 0xE0..0xFD are combining diacritics that PRECEDE the base letter in
//     ANSEL but FOLLOW it in Unicode, so the converter swaps the order.
//   - GEDCOM extends the base at 0xBE/0xBF (boxes), 0xCD/0xCE (midline
//     e/o, approximated with combining macron) and 0xCF (ß).
var anselBase = map[byte]string{ // 0xA0..
	0xA1: "Ł", 0xA2: "Ø", 0xA3: "Đ", 0xA4: "Þ", 0xA5: "Æ", 0xA6: "Œ",
	0xA7: "ʹ", 0xA8: "·", 0xA9: "♭", 0xAA: "®", 0xAB: "±", 0xAC: "Ơ",
	0xAD: "Ư", 0xAE: "ʼ",
	0xB0: "ʻ", 0xB1: "ł", 0xB2: "ø", 0xB3: "đ", 0xB4: "þ", 0xB5: "æ",
	0xB6: "œ", 0xB7: "ʺ", 0xB8: "ı", 0xB9: "£", 0xBA: "ð", 0xBC: "ơ",
	0xBD: "ư",
	0xBE: "□", 0xBF: "■",
	0xC0: "°", 0xC1: "ℓ", 0xC2: "℗", 0xC3: "©", 0xC4: "♯", 0xC5: "¿",
	0xC6: "¡",
	0xCD: "ē", // GEDCOM "midline e"
	0xCE: "ō", // GEDCOM "midline o"
	0xCF: "ß",
}

// combining diacritics for 0xE0..0xFD, in byte order (empty = unmapped).
var anselCombining = map[byte]string{
	0xE0: "\u0309", // hook above
	0xE1: "\u0300", // grave
	0xE2: "\u0301", // acute
	0xE3: "\u0302", // circumflex
	0xE4: "\u0303", // tilde
	0xE5: "\u0304", // macron
	0xE6: "\u0306", // breve
	0xE7: "\u0307", // dot above
	0xE8: "\u0308", // dieresis
	0xE9: "\u030C", // caron
	0xEA: "\u030A", // ring above
	0xEB: "\uFE20", // ligature left half
	0xEC: "\uFE21", // ligature right half
	0xED: "\u0315", // comma above right
	0xEE: "\u030B", // double acute
	0xEF: "\u0310", // candrabindu
	0xF0: "\u0327", // cedilla
	0xF1: "\u0328", // ogonek
	0xF2: "\u0323", // dot below
	0xF3: "\u0324", // dieresis below
	0xF4: "\u0325", // ring below
	0xF5: "\u0333", // double low line
	0xF6: "\u0332", // low line
	0xF7: "\u0326", // comma below
	0xF8: "\u031C", // transliteration half-ring below
	0xF9: "\u032E", // breve below
	0xFA: "\uFE22", // tilde left half
	0xFB: "\uFE23", // tilde right half
	0xFC: "\u0338", // slash through (GEDCOM)
	0xFE: "\u0313", // comma above
}

// anselToUTF8 converts ANSEL bytes to UTF-8. Unmapped bytes become '?'.
func anselToUTF8(raw []byte, warnings *[]string) []byte {
	var b bytes.Buffer
	bad := 0
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		switch {
		case c < 0x80:
			b.WriteByte(c)
		case c >= 0xE0 && c <= 0xFE:
			// Combining diacritic: ANSEL puts it BEFORE the base letter.
			if i+1 < len(raw) {
				i++
				b.WriteString(string(raw[i]))
				b.WriteString(anselCombining[c])
			} else {
				bad++
			}
		default:
			if s, ok := anselBase[c]; ok {
				b.WriteString(s)
			} else {
				bad++
				b.WriteByte('?')
			}
		}
	}
	if bad > 0 {
		*warnings = append(*warnings,
			fmt.Sprintf("charset: ANSEL had %d unmapped byte(s), replaced with '?'", bad))
	}
	return b.Bytes()
}
