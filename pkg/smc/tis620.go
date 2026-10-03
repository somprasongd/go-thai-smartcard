package smc

import (
	"strings"
	"unicode/utf8"
)

// decodeTIS620 keeps card text valid UTF-8 even when the reader returns an
// undefined byte. Character ranges follow TIS-620, not Windows-874 or the
// ISO-8859-11 extension that assigns a non-breaking space to 0xA0.
// Mapping reference: https://www.unicode.org/Public/MAPPINGS/ISO8859/8859-11.TXT
func decodeTIS620(data []byte) string {
	var text strings.Builder
	text.Grow(len(data))
	for _, value := range data {
		switch {
		case value < 0x80:
			text.WriteByte(value)
		case value >= 0xA1 && value <= 0xDA:
			text.WriteRune('\u0E01' + rune(value-0xA1))
		case value >= 0xDF && value <= 0xFB:
			text.WriteRune('\u0E3F' + rune(value-0xDF))
		default:
			text.WriteRune(utf8.RuneError)
		}
	}
	return text.String()
}
