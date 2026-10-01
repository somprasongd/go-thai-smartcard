package util

// respCmdLegacy and respCmdExtended are the two GET RESPONSE variants used by
// the Thai ID applet. They are kept here, free of any PC/SC dependency, so
// that callers of the previously exported util.GetResponseCommand keep
// working now that the PC/SC helpers moved to pkg/transport/pcsc.
var (
	respCmdLegacy   = []byte{0x00, 0xc0, 0x00, 0x00}
	respCmdExtended = []byte{0x00, 0xc0, 0x00, 0x01}
)

// GetResponseCommand picks the GET RESPONSE variant to use for a card. The two
// variants differ only in P2, and the requested length is appended as Le, so
// the result is a five byte case 4 APDU.
//
// Legacy is the default because that is what real hardware uses. A Thai
// national ID card captured with cmd/record answered with ATR
//
//	3B 79 96 00 00 54 48 20 4E 49 44 20 31 38
//
// whose historical bytes spell "TH NID 18". Byte 1 is 0x79, not 0x67, so the
// card takes the legacy path and its GET RESPONSE goes out as 00 C0 00 00 <le>.
// Collapsing P2 and Le into one byte there is what a card rejects, so the
// length must stay a byte of its own.
//
// The 3B 67 prefix selects the extended form. It arrived with the original
// implementation and is kept for compatibility, but it has not been observed on
// any card here, so treat it as unverified rather than as the normal case. This
// comment previously claimed Thai ID cards answer with 3B 67, which the capture
// above contradicts.
//
// A short or nil ATR used to index out of range here, so the length is checked
// first.
func GetResponseCommand(atr []byte) []byte {
	if len(atr) >= 2 && atr[0] == 0x3B && atr[1] == 0x67 {
		return respCmdExtended
	}
	return respCmdLegacy
}
