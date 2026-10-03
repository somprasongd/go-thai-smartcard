package smc

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/somprasongd/go-thai-smartcard/pkg/transport"
	"github.com/somprasongd/go-thai-smartcard/pkg/util"
)

// ErrCardNil is returned when a reader is asked to read without a card.
var ErrCardNil = errors.New("card is nil")

// statusWordLength is the SW1/SW2 trailer every successful applet response
// ends with. It is stripped before the payload is decoded.
const statusWordLength = 2

// getResponsePrefixLength is CLA+INS+P1+P2: the four byte prefix that the
// requested length is appended to.
const getResponsePrefixLength = 4

// getResponseDefaultPrefix is the legacy GET RESPONSE variant, used when
// respCmd does not carry a full command of its own.
var getResponseDefaultPrefix = []byte{0x00, 0xc0, 0x00, 0x00}

// laserGetResponseLen is the length byte used for the laser code. It is 0x10
// rather than the last byte of the command, matching the Java implementation.
const laserGetResponseLen = 0x10

// statusMoreData is SW1 of 61 xx, the card reporting that xx bytes are ready
// for GET RESPONSE. It is the only first response a field command may carry:
// the card answered 61 xx on all 41 first responses of the session captured
// with cmd/record, and never anything else.
const statusMoreData = 0x61

// statusCommandOK is SW1 of 90 00, the status word that ends every successful
// GET RESPONSE.
const statusCommandOK = 0x90

// reader holds a connected card together with the GET RESPONSE variant
// selected for it. Every applet reader in this package embeds it.
type reader struct {
	card    transport.Card
	respCmd []byte
}

func newReader(card transport.Card, respCmd []byte) *reader {
	if respCmd == nil {
		respCmd = util.GetResponseCommand(nil)
	}
	return &reader{card: card, respCmd: respCmd}
}

// status confirms the card is still in place before an exchange.
func (r *reader) status() error {
	if r.card == nil {
		return ErrCardNil
	}
	_, err := r.card.Status()
	return err
}

// readData reads an ASCII field.
func (r *reader) readData(cmd []byte) (string, error) {
	if len(cmd) == 0 {
		return "", errors.New("readData: empty apdu")
	}
	payload, err := r.payload(cmd, cmd[len(cmd)-1])
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(payload)), nil
}

// readDataThai reads a TIS-620 encoded field and converts it to UTF-8.
func (r *reader) readDataThai(cmd []byte) (string, error) {
	if len(cmd) == 0 {
		return "", errors.New("readDataThai: empty apdu")
	}
	payload, err := r.payload(cmd, cmd[len(cmd)-1])
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(decodeTIS620(payload)), nil
}

// readLaserData reads the laser code, which has its own GET RESPONSE length
// and its own trailing padding rule.
func (r *reader) readLaserData(cmd []byte) (string, error) {
	if len(cmd) == 0 {
		return "", errors.New("readLaserData: empty apdu")
	}
	payload, err := r.payload(cmd, laserGetResponseLen)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(bytes.Trim(payload, "\x00"))), nil
}

// payload performs the two step exchange the applet uses — request the field,
// then fetch it with GET RESPONSE — and returns the bytes the card sent, minus
// the status word.
//
// The status words are the only thing it interprets, and strictly: the field
// command must answer 61 xx — the card reporting xx bytes ready for GET
// RESPONSE — and the GET RESPONSE must end in 90 00. A field command answered
// with anything else fails the field instead of being ignored and getting a
// GET RESPONSE sent at it, which used to surface the failure as a parse error
// on whatever the card said next, if it surfaced at all.
//
// The payload bytes are still interpreted by nobody here. The text readers
// above trim what a human would call padding, which is right for a fixed
// width text field and wrong for anything binary: a 0x20 is an ordinary data
// byte in a JPEG, and trimming it misaligns every byte that follows. A payload
// that happens to be all whitespace would even come back empty, which the face
// image reader takes as end of image.
func (r *reader) payload(cmd []byte, getResponseLen byte) ([]byte, error) {
	if err := r.status(); err != nil {
		return nil, err
	}

	first, err := r.card.Transmit(cmd)
	if err != nil {
		return nil, err
	}
	sw, ok := trailingStatusWord(first)
	if !ok {
		return nil, fmt.Errorf("read: field command response too short (%d bytes, want a status word)", len(first))
	}
	if sw[0] != statusMoreData {
		return nil, fmt.Errorf("read: unexpected status % x from the field command, want 61 xx", sw)
	}

	rsp, err := r.card.Transmit(r.getResponseAPDU(getResponseLen))
	if err != nil {
		return nil, err
	}

	// A successful response always carries SW1/SW2. A shorter one is a
	// protocol violation, not an empty field, and used to panic here.
	sw, ok = trailingStatusWord(rsp)
	if !ok {
		return nil, fmt.Errorf("read: response too short (%d bytes, want at least %d)", len(rsp), statusWordLength)
	}
	if sw[0] != statusCommandOK || sw[1] != 0x00 {
		return nil, fmt.Errorf("read: unexpected status % x from GET RESPONSE, want 90 00", sw)
	}
	return rsp[:len(rsp)-statusWordLength], nil
}

// trailingStatusWord returns the final SW1/SW2 of a response, or false when
// the response is too short to carry one.
func trailingStatusWord(rsp []byte) ([]byte, bool) {
	if len(rsp) < statusWordLength {
		return nil, false
	}
	return rsp[len(rsp)-statusWordLength:], true
}

// getResponseAPDU builds 00 C0 00 <p2> <le> into a fresh slice. Copying rather
// than appending onto respCmd keeps the shared GET RESPONSE command unaliased,
// since pkg/util hands out one package level slice per variant.
//
// P2 and Le are both on the wire. GET RESPONSE is a case 4 APDU whose Le tells
// the card how many bytes are wanted, and the legacy and extended variants
// differ only in P2. Collapsing the two into one byte, as 00 C0 00 <le> does,
// sends a case 2 command: <le> is then read as P2, the card is never told how
// much to return, and the length sensitive reads (the laser code above all)
// come back with the wrong amount of data or fail outright.
func (r *reader) getResponseAPDU(le byte) []byte {
	prefix := r.respCmd
	if len(prefix) < getResponsePrefixLength {
		prefix = getResponseDefaultPrefix
	}
	apdu := make([]byte, 0, len(prefix)+1)
	apdu = append(apdu, prefix...)
	return append(apdu, le)
}
