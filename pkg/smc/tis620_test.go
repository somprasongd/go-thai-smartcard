package smc

import (
	"testing"
	"unicode/utf8"
)

func TestDecodeTIS620(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
		want string
	}{
		{"empty", nil, ""},
		{"ASCII and padding", []byte("  ABC#123\x00\t\n"), "  ABC#123\x00\t\n"},
		{"Thai consonants", []byte{0xA1, 0xA2, 0xCE}, "กขฮ"},
		{"vowels and tone marks", []byte{0xA1, 0xD4, 0xE8, 0xA7}, "กิ่ง"},
		{"Thai digits and currency", []byte{0xDF, 0xF0, 0xF1, 0xF9}, "฿๐๑๙"},
		{"range boundaries", []byte{0xA1, 0xDA, 0xDF, 0xFB}, "กฺ฿๛"},
		{"undefined bytes", []byte{0x80, 0x9F, 0xA0, 0xDB, 0xDE, 0xFC, 0xFF}, "�������"},
		{"mixed text", []byte{0x41, 0x23, 0xA1, 0xFF, 0x31}, "A#ก�1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := decodeTIS620(tc.data); got != tc.want {
				t.Fatalf("decodeTIS620(% x) = %q, want %q", tc.data, got, tc.want)
			}
		})
	}
}

func TestDecodeTIS620EveryByteIsValidUTF8(t *testing.T) {
	for value := 0; value < 256; value++ {
		got := decodeTIS620([]byte{byte(value)})
		if !utf8.ValidString(got) || utf8.RuneCountInString(got) != 1 {
			t.Fatalf("byte %02x decoded to invalid or multiple characters: %q", value, got)
		}
		if value >= 0x80 && !(value >= 0xA1 && value <= 0xDA) && !(value >= 0xDF && value <= 0xFB) && got != "�" {
			t.Fatalf("undefined byte %02x decoded to %q", value, got)
		}
	}
}

func TestDecodeTIS620ThaiRepertoire(t *testing.T) {
	// Expected characters are in the standard's byte order, including the
	// obsolete consonants and combining marks that ordinary names rarely use.
	want := []rune("กขฃคฅฆงจฉชซฌญฎฏฐฑฒณดตถทธนบปผฝพฟภมยรฤลฦวศษสหฬอฮฯะัาำิีึืฺุู฿เแโใไๅๆ็่้๊๋์ํ๎๏๐๑๒๓๔๕๖๗๘๙๚๛")
	var data []byte
	for value := 0xA1; value <= 0xFB; value++ {
		if value < 0xDB || value >= 0xDF {
			data = append(data, byte(value))
		}
	}
	if got := decodeTIS620(data); got != string(want) {
		t.Fatalf("Thai repertoire = %q, want %q", got, string(want))
	}
}
