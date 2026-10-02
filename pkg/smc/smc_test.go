package smc_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/somprasongd/go-thai-smartcard/pkg/apdu"
	"github.com/somprasongd/go-thai-smartcard/pkg/model"
	"github.com/somprasongd/go-thai-smartcard/pkg/smc"
	"github.com/somprasongd/go-thai-smartcard/pkg/transport"
)

// Synthetic payloads, encoded as TIS-620 because that is what the applet
// returns and what the library decodes. Every value is made up: nothing here
// comes from a real card, and no real card data belongs in the repository.
const (
	payloadNameThai = "b9d2c223cac1aad2c22323e3a8b4d5"       // นาย#สมชาย##ใจดี
	payloadNameEng  = "4d5223534f4d4348414923234a4149444545" // MR#SOMCHAI##JAIDEE
	payloadIssuer   = "a1c3c1a1d2c3bbc5cdb4bbc3d0aad2aab9"   // กรมการปลอดประชาชน
	payloadAddress  = "3132332f343523cbc1d9e8b7d5e8203423b6b9b9cad5cbc5d2a720313223b5d3bac5a4c5cda7b5d2cbc5d1a723cdd3e0c0cda4c5cda7b5d2cbc5d1a723a8d1a7cbc7d1b4a1c3d8a7e0b7bec1cbd2b9a4c3"
	// payloadLaser is 0x10 bytes: two letters, ten digits and four NULs of
	// padding, the shape the real card sends for the laser GET RESPONSE and
	// readLaserData trims.
	payloadLaser      = "41423132333435363738393000000000"                             // AB1234567890 + NUL padding
	payloadNhsoMain   = "333020bad2b7"                                                 // 30 บาท
	payloadNhsoSub    = "bbc3d0a1d1b9cad1a7a4c1"                                       // ประกันสังคม
	payloadNhsoMainH  = "e2c3a7bec2d2bad2c5cad4c3d4c3d1a1c9ec"                         // โรงพยาบาลสิริรักษ์
	payloadNhsoSubH   = "e2c3a7bec2d2bad2c5cac1e0b4e7a8bec3d0bac3c1c3d2aad2b9d8e0bac9" // โรงพยาบาลสมเด็จพระบรมราชานุเบษ
	payloadNhsoPaid   = "a4c3d1e9a7"                                                   // ครั้ง
	payloadNhsoAmount = "32"                                                           // 2
	payloadNhsoDate   = "3235363230353135"                                             // 25620515 -> 2019-05-15
	payloadCID        = "31323334353637383930313233"                                   // 1234567890123
	payloadGender     = "31"                                                           // 1
	payloadDob        = "3235333731323331"                                             // 25371231 -> 1994-12-31
	payloadIssueDate  = "3235363230353135"                                             // 25620515 -> 2019-05-15
	payloadExpireDate = "3235363930353135"                                             // 25690515 -> 2026-05-15
	payloadImageChunk = "ffd8ff"                                                       // JPEG start of image
)

// atrThaiID is the ATR a real Thai national ID card answered with when a trace
// was captured with cmd/record. The historical bytes spell "TH NID 18".
//
// Byte 1 is 0x79, so this card takes the legacy GET RESPONSE variant. The
// synthetic traces used to carry a made up 3B 67 ATR instead, which meant they
// exercised only the extended path; matching the capture means a regression in
// the legacy path fails these tests too.
const atrThaiID = "3b799600005448204e4944203138"

// traceBuilder assembles a trace that mirrors what the library puts on the
// wire: one applet SELECT, then a command/GET RESPONSE pair per field.
type traceBuilder struct {
	trace *transport.Trace
}

func newTraceBuilder() *traceBuilder {
	return &traceBuilder{trace: &transport.Trace{
		Name: "synthetic",
		Note: "SYNTHETIC. Not captured from a real card. Generated so the card logic can be tested without hardware; use cmd/record to capture a real one.",
		Atr:  atrThaiID,
	}}
}

// getResponsePrefix is the GET RESPONSE command the library sends for
// atrThaiID: 00 C0 00 00, followed by the requested length. GET RESPONSE is a
// case 4 APDU, so P2 and Le are separate bytes and both belong on the wire.
// The zero P2 is the legacy variant, which is the one atrThaiID selects.
//
// It is written out as a literal rather than derived from util.GetResponseCommand
// so that a change to the bytes the library transmits has to be made
// deliberately here as well. The fake card replays strictly, so a mismatch
// fails the read rather than returning quietly different data.
const getResponsePrefix = "00c00000"

// getResponse builds 00 C0 00 00 <le>, where le is the last byte of the
// command, which is what the library sends.
func getResponse(cmd []byte) []byte {
	return mustDecodeHex(getResponsePrefix + hex.EncodeToString(cmd[len(cmd)-1:]))
}

func (b *traceBuilder) selectApplet(cmd []byte) {
	b.exchange(cmd, []byte{0x90, 0x00})
}

// readField records a command and the GET RESPONSE that fetches its payload.
func (b *traceBuilder) readField(cmd []byte, payloadHex string) {
	payload := mustDecodeHex(payloadHex)
	// 61 xx is the card reporting that xx bytes are ready for GET RESPONSE.
	// That is what a real card answers — the capture holds 41 first responses
	// and every one of them is 61 xx; no 6C ever appears as a status word.
	b.exchange(cmd, []byte{0x61, byte(len(payload))})
	b.exchange(getResponse(cmd), append(append([]byte(nil), payload...), 0x90, 0x00))
}

// readLaser records the laser exchange, which uses length 0x10 rather than the
// last byte of the command. The payload is what the card sends back for that
// length: exactly 0x10 bytes, so the first response reports 61 10 as the
// capture shows.
func (b *traceBuilder) readLaser(cmd []byte, payloadHex string) {
	payload := mustDecodeHex(payloadHex)
	b.exchange(cmd, []byte{0x61, byte(len(payload))})
	b.exchange(mustDecodeHex(getResponsePrefix+"10"), append(append([]byte(nil), payload...), 0x90, 0x00))
}

func (b *traceBuilder) exchange(cmd, rsp []byte) {
	b.trace.Exchanges = append(b.trace.Exchanges, transport.Exchange{
		Command:  hex.EncodeToString(cmd),
		Response: hex.EncodeToString(rsp),
	})
}

func mustDecodeHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

// personalTrace emits the exchanges that a personal read issues, including the
// face image chunks when asked for.
func personalTrace(withImage bool) *transport.Trace {
	b := newTraceBuilder()
	appendPersonal(b, withImage)
	return b.trace
}

// laserTrace emits the personal read followed by the laser applet.
func laserTrace() *transport.Trace {
	b := newTraceBuilder()
	appendPersonal(b, false)
	b.selectApplet(apdu.CardCMD.Select)
	b.readLaser(apdu.CardCMD.LaserId, payloadLaser)
	return b.trace
}

// nhsoTrace emits the personal read followed by the NHSO applet.
func nhsoTrace() *transport.Trace {
	b := newTraceBuilder()
	appendPersonal(b, false)
	b.selectApplet(apdu.NhsoCMD.Select)
	b.readField(apdu.NhsoCMD.MainInscl, payloadNhsoMain)
	b.readField(apdu.NhsoCMD.SubInscl, payloadNhsoSub)
	b.readField(apdu.NhsoCMD.MainHospitalName, payloadNhsoMainH)
	b.readField(apdu.NhsoCMD.SubHospitalName, payloadNhsoSubH)
	b.readField(apdu.NhsoCMD.PaidType, payloadNhsoPaid)
	b.readField(apdu.NhsoCMD.IssueDate, payloadNhsoDate)
	b.readField(apdu.NhsoCMD.ExpireDate, payloadNhsoDate)
	b.readField(apdu.NhsoCMD.UpdateDate, payloadNhsoDate)
	b.readField(apdu.NhsoCMD.ChangeHospitalAmount, payloadNhsoAmount)
	return b.trace
}

// fullTrace emits every applet, which is what the agent reads by default.
func fullTrace() *transport.Trace {
	b := newTraceBuilder()
	appendPersonal(b, true)
	b.selectApplet(apdu.CardCMD.Select)
	b.readLaser(apdu.CardCMD.LaserId, payloadLaser)
	b.selectApplet(apdu.NhsoCMD.Select)
	b.readField(apdu.NhsoCMD.MainInscl, payloadNhsoMain)
	b.readField(apdu.NhsoCMD.SubInscl, payloadNhsoSub)
	b.readField(apdu.NhsoCMD.MainHospitalName, payloadNhsoMainH)
	b.readField(apdu.NhsoCMD.SubHospitalName, payloadNhsoSubH)
	b.readField(apdu.NhsoCMD.PaidType, payloadNhsoPaid)
	b.readField(apdu.NhsoCMD.IssueDate, payloadNhsoDate)
	b.readField(apdu.NhsoCMD.ExpireDate, payloadNhsoDate)
	b.readField(apdu.NhsoCMD.UpdateDate, payloadNhsoDate)
	b.readField(apdu.NhsoCMD.ChangeHospitalAmount, payloadNhsoAmount)
	return b.trace
}

func appendPersonal(b *traceBuilder, withImage bool) {
	b.selectApplet(apdu.PersonalCMD.Select)
	b.readField(apdu.PersonalCMD.Cid, payloadCID)
	b.readField(apdu.PersonalCMD.NameThai, payloadNameThai)
	b.readField(apdu.PersonalCMD.NameEng, payloadNameEng)
	b.readField(apdu.PersonalCMD.Dob, payloadDob)
	b.readField(apdu.PersonalCMD.Gender, payloadGender)
	b.readField(apdu.PersonalCMD.CardIssuer, payloadIssuer)
	b.readField(apdu.PersonalCMD.IssueDate, payloadIssueDate)
	b.readField(apdu.PersonalCMD.ExpireDate, payloadExpireDate)
	b.readField(apdu.PersonalCMD.Address, payloadAddress)
	if withImage {
		for _, cmd := range apdu.PersonalCMD.FaceImage {
			b.readField(cmd, payloadImageChunk)
		}
	}
}

// newTestReader wires a smart card onto a fake transport replaying trace.
func newTestReader(t *testing.T, trace *transport.Trace) (*smc.SmartCard, *transport.FakeCard) {
	t.Helper()

	card, err := transport.NewFakeCard(trace)
	if err != nil {
		t.Fatalf("NewFakeCard: %v", err)
	}
	tr := transport.NewFakeTransport([]string{"Fake Reader 0"}, card)
	t.Cleanup(func() { _ = tr.Close() })

	return smc.NewSmartCardWith(tr), card
}

func TestReadPersonal(t *testing.T) {
	reader, card := newTestReader(t, personalTrace(false))

	data, err := reader.Read(nil, &smc.Options{ShowFaceImage: false})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if data == nil || data.Personal == nil {
		t.Fatalf("no personal data returned: %+v", data)
	}

	p := data.Personal
	if p.Cid != "1234567890123" {
		t.Errorf("Cid = %q, want %q", p.Cid, "1234567890123")
	}
	if p.Name.FirstName != "สมชาย" || p.Name.LastName != "ใจดี" {
		t.Errorf("Name = %+v", p.Name)
	}
	if p.Name.FullName != "นายสมชาย ใจดี" {
		t.Errorf("Name.FullName = %q", p.Name.FullName)
	}
	// The prefix is concatenated onto the first name with no separator, so
	// the English full name reads "MRSOMCHAI" rather than "MR SOMCHAI".
	if p.NameEng.FullName != "MRSOMCHAI JAIDEE" {
		t.Errorf("NameEng.FullName = %q", p.NameEng.FullName)
	}
	if p.Dob != "1994-12-31" {
		t.Errorf("Dob = %q, want %q", p.Dob, "1994-12-31")
	}
	if p.Gender != "1" {
		t.Errorf("Gender = %q", p.Gender)
	}
	if p.CardIssuer != "กรมการปลอดประชาชน" {
		t.Errorf("CardIssuer = %q", p.CardIssuer)
	}
	if p.IssueDate != "2019-05-15" {
		t.Errorf("IssueDate = %q", p.IssueDate)
	}
	if p.ExpireDate != "2026-05-15" {
		t.Errorf("ExpireDate = %q", p.ExpireDate)
	}
	if p.Address.HouseNo != "123/45" || p.Address.Province != "กรุงเทพมหานคร" {
		t.Errorf("Address = %+v", p.Address)
	}
	if p.FaceImage != "" {
		t.Error("FaceImage should be empty when ShowFaceImage is off")
	}
	if data.Card != nil {
		t.Error("Card should be nil when ShowLaserData is off")
	}
	if data.Nhso != nil {
		t.Error("Nhso should be nil when ShowNhsoData is off")
	}
	if !card.Disconnected() {
		t.Error("Read must disconnect the card when it is done")
	}
}

func TestReadPersonalWithFaceImage(t *testing.T) {
	reader, _ := newTestReader(t, personalTrace(true))

	data, err := reader.Read(nil, &smc.Options{ShowFaceImage: true})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if data.Personal.FaceImage == "" {
		t.Fatal("FaceImage is empty")
	}
	// Each of the 20 chunks holds the 3 byte JPEG start marker FF D8 FF.
	// The chunks are concatenated as hex, decoded, then base64 encoded, so
	// each chunk contributes 4 base64 characters.
	const chunkB64 = "/9j/"
	want := len(chunkB64) * len(apdu.PersonalCMD.FaceImage)
	if len(data.Personal.FaceImage) != want {
		t.Errorf("FaceImage length = %d, want %d", len(data.Personal.FaceImage), want)
	}
	if !strings.HasPrefix(data.Personal.FaceImage, chunkB64) {
		t.Errorf("FaceImage = %q, want it to start with %q", data.Personal.FaceImage, chunkB64)
	}
}

// imageChunksWhitespace carries a portrait that puts whitespace exactly where
// the old text read path trimmed it: one chunk ends on a 0x20, the next starts
// on one, and the one after is nothing but 0x20. The image ends at FFD9, which
// is followed by the padding the card adds to fill the chunk, and then by a
// chunk the card never really sent.
//
// Under strings.TrimSpace the trailing byte of the first chunk and the leading
// byte of the second are eaten, which misaligns everything after them, and the
// all-space chunk comes back empty, which ReadFaceImage takes as the end of the
// image. The result no longer decodes.
var imageChunksWhitespace = []string{
	"ffd8aabb",   // start of image
	"ccdd20",     // ends on a space that is data
	"2020ee",     // starts on a space that is data
	"202020",     // entirely spaces, and still inside the image
	"1122ffd9",   // end of image
	"20202020",   // padding the card adds to fill out the chunk
	"9999999999", // past the end of the image
}

// imageWhitespace is the portrait those chunks carry: everything up to and
// including FFD9, with the padding and the chunk past it left out.
const imageWhitespace = "ffd8aabbccdd202020ee2020201122ffd9"

// faceImageTrace emits a personal read whose face image chunks are the given
// ones. Any chunk beyond the list returns a single 00, standing in for a card
// that has already finished sending and pads the remaining chunks.
func faceImageTrace(chunks ...string) *transport.Trace {
	b := newTraceBuilder()
	b.selectApplet(apdu.PersonalCMD.Select)
	b.readField(apdu.PersonalCMD.Cid, payloadCID)
	b.readField(apdu.PersonalCMD.NameThai, payloadNameThai)
	b.readField(apdu.PersonalCMD.NameEng, payloadNameEng)
	b.readField(apdu.PersonalCMD.Dob, payloadDob)
	b.readField(apdu.PersonalCMD.Gender, payloadGender)
	b.readField(apdu.PersonalCMD.CardIssuer, payloadIssuer)
	b.readField(apdu.PersonalCMD.IssueDate, payloadIssueDate)
	b.readField(apdu.PersonalCMD.ExpireDate, payloadExpireDate)
	b.readField(apdu.PersonalCMD.Address, payloadAddress)
	for i, cmd := range apdu.PersonalCMD.FaceImage {
		chunk := "00"
		if i < len(chunks) {
			chunk = chunks[i]
		}
		b.readField(cmd, chunk)
	}
	return b.trace
}

// TestReadFaceImageKeepsWhitespaceBytes is the regression test for the portrait
// losing bytes: every 0x20 that is part of the image has to survive, and only
// the padding after the end of image may be dropped.
func TestReadFaceImageKeepsWhitespaceBytes(t *testing.T) {
	reader, _ := newTestReader(t, faceImageTrace(imageChunksWhitespace...))

	data, err := reader.Read(nil, &smc.Options{ShowFaceImage: true})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	got, err := base64.StdEncoding.DecodeString(data.Personal.FaceImage)
	if err != nil {
		t.Fatalf("FaceImage is not valid base64: %v", err)
	}
	want := mustDecodeHex(imageWhitespace)
	if !bytes.Equal(got, want) {
		t.Errorf("FaceImage = %x\n want  %x", got, want)
	}
}

// TestReadFaceImageStopsAtEndOfImage covers a card that sends fewer chunks than
// the library asks for. The image ends partway through, and the chunk after it
// carries data as well as padding: neither may reach the result. A read that
// merely trimmed trailing whitespace would swallow the padding, keep the data
// and carry on, so the two are told apart by that byte.
func TestReadFaceImageStopsAtEndOfImage(t *testing.T) {
	reader, _ := newTestReader(t, faceImageTrace("ffd8aabb", "ccddffd9", "ee20"))

	data, err := reader.Read(nil, &smc.Options{ShowFaceImage: true})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	got, err := base64.StdEncoding.DecodeString(data.Personal.FaceImage)
	if err != nil {
		t.Fatalf("FaceImage is not valid base64: %v", err)
	}
	want := mustDecodeHex("ffd8aabbccddffd9")
	if !bytes.Equal(got, want) {
		t.Errorf("FaceImage = %x, want %x", got, want)
	}
}

func TestReadLaser(t *testing.T) {
	reader, _ := newTestReader(t, laserTrace())

	data, err := reader.Read(nil, &smc.Options{ShowLaserData: true})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if data.Card == nil {
		t.Fatal("Card should be present when ShowLaserData is on")
	}
	if data.Card.LaserId != "AB1234567890" {
		t.Errorf("LaserId = %q", data.Card.LaserId)
	}
	if data.Nhso != nil {
		t.Error("Nhso should be nil when ShowNhsoData is off")
	}
}

func TestReadNhso(t *testing.T) {
	reader, _ := newTestReader(t, nhsoTrace())

	data, err := reader.Read(nil, &smc.Options{ShowNhsoData: true})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if data.Nhso == nil {
		t.Fatal("Nhso should be present when ShowNhsoData is on")
	}
	n := data.Nhso
	if n.MainInscl != "30 บาท" {
		t.Errorf("MainInscl = %q", n.MainInscl)
	}
	if n.SubInscl != "ประกันสังคม" {
		t.Errorf("SubInscl = %q", n.SubInscl)
	}
	if n.MainHospitalName != "โรงพยาบาลสิริรักษ์" {
		t.Errorf("MainHospitalName = %q", n.MainHospitalName)
	}
	if n.IssueDate != "2019-05-15" {
		t.Errorf("IssueDate = %q", n.IssueDate)
	}
	if n.ChangeHospitalAmount != "2" {
		t.Errorf("ChangeHospitalAmount = %q", n.ChangeHospitalAmount)
	}
}

func TestReadEverything(t *testing.T) {
	reader, _ := newTestReader(t, fullTrace())

	data, err := reader.Read(nil, &smc.Options{ShowFaceImage: true, ShowLaserData: true, ShowNhsoData: true})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if data.Personal == nil || data.Card == nil || data.Nhso == nil {
		t.Fatalf("expected all three sections, got %+v", data)
	}
}

func TestReadDefaultOptions(t *testing.T) {
	reader, _ := newTestReader(t, personalTrace(true))

	// A nil Options means face image on, everything else off.
	data, err := reader.Read(nil, nil)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if data.Personal == nil {
		t.Fatal("no personal data returned")
	}
	if data.Card != nil || data.Nhso != nil {
		t.Error("default options should not read the card or nhso applets")
	}
}

// A read whose GET RESPONSE is too short to carry a status word must not take
// the process down. Individual field failures are logged and skipped, so the
// read still completes with an empty value for that field.
func TestReadShortResponseIsSurvivable(t *testing.T) {
	b := newTraceBuilder()
	b.selectApplet(apdu.PersonalCMD.Select)
	// 61 says a payload is ready, but GET RESPONSE returns a single byte:
	// shorter than the two byte status word.
	b.exchange(apdu.PersonalCMD.Cid, []byte{0x61, 0x0d})
	b.exchange(getResponse(apdu.PersonalCMD.Cid), []byte{0x00})
	b.readField(apdu.PersonalCMD.NameThai, payloadNameThai)
	b.readField(apdu.PersonalCMD.NameEng, payloadNameEng)
	b.readField(apdu.PersonalCMD.Dob, payloadDob)
	b.readField(apdu.PersonalCMD.Gender, payloadGender)
	b.readField(apdu.PersonalCMD.CardIssuer, payloadIssuer)
	b.readField(apdu.PersonalCMD.IssueDate, payloadIssueDate)
	b.readField(apdu.PersonalCMD.ExpireDate, payloadExpireDate)
	b.readField(apdu.PersonalCMD.Address, payloadAddress)

	reader, _ := newTestReader(t, b.trace)

	data, err := reader.Read(nil, &smc.Options{})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if data.Personal.Cid != "" {
		t.Errorf("Cid = %q, want empty", data.Personal.Cid)
	}
	if data.Personal.NameEng.FullName != "MRSOMCHAI JAIDEE" {
		t.Errorf("fields after the broken one should still read, got %q", data.Personal.NameEng.FullName)
	}
}

// A command sequence that drifts from the trace must surface as an error on
// the field it affects rather than as silently different data.
func TestReadApduDriftIsDetected(t *testing.T) {
	trace := personalTrace(false)
	// Exchange 1 is the CID command itself.
	trace.Exchanges[1].Command = "deadbeef"

	reader, _ := newTestReader(t, trace)

	data, err := reader.Read(nil, &smc.Options{})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if data.Personal.Cid != "" {
		t.Errorf("Cid = %q, want empty because the command no longer matches the trace", data.Personal.Cid)
	}
}

// capturingCard records every command it is handed so a test can assert on the
// exact bytes that reach the wire, independent of any trace fixture.
type capturingCard struct {
	atr  []byte
	cmds [][]byte
}

func (c *capturingCard) Status() (transport.Status, error) {
	return transport.Status{Atr: c.atr}, nil
}

func (c *capturingCard) Transmit(cmd []byte) ([]byte, error) {
	c.cmds = append(c.cmds, append([]byte(nil), cmd...))
	if len(c.cmds)%2 == 0 {
		// Even exchanges are GET RESPONSE: a two byte payload plus 9000.
		return []byte{0x41, 0x42, 0x90, 0x00}, nil
	}
	// Odd exchanges are the field command: 61 02 means two bytes are ready.
	return []byte{0x61, 0x02}, nil
}

func (c *capturingCard) Disconnect() error { return nil }

// The GET RESPONSE has to stay a case 4 APDU, so P2 and Le stay separate
// bytes: collapsing them would send 00 C0 00 <le>, a case 2 command in which
// the card is never told how many bytes to return. The synthetic traces above
// already enforce this through strict replay, but they are built from the same
// assumption as the implementation, so the bytes are pinned literally here.
func TestGetResponseApduKeepsP2AndLe(t *testing.T) {
	atr := mustDecodeHex(atrThaiID)
	tests := []struct {
		name    string
		respCmd []byte
		read    func(transport.Card, []byte)
		wantGet string
	}{
		{
			name:    "laser read uses the extended variant and length 10",
			respCmd: []byte{0x00, 0xc0, 0x00, 0x01},
			read: func(card transport.Card, respCmd []byte) {
				smc.NewCardReader(card, respCmd).ReadLaserId()
			},
			wantGet: "00c0000110",
		},
		{
			name:    "laser read honours the legacy variant",
			respCmd: []byte{0x00, 0xc0, 0x00, 0x00},
			read: func(card transport.Card, respCmd []byte) {
				smc.NewCardReader(card, respCmd).ReadLaserId()
			},
			wantGet: "00c0000010",
		},
		{
			name:    "field read takes its length from the command",
			respCmd: []byte{0x00, 0xc0, 0x00, 0x01},
			read: func(card transport.Card, respCmd []byte) {
				smc.NewPersonalReader(card, respCmd).ReadCID()
			},
			// The CID command ends in 0d.
			wantGet: "00c000010d",
		},
		{
			name:    "a respCmd that is not a full command falls back to legacy",
			respCmd: []byte{0x00, 0xc0},
			read: func(card transport.Card, respCmd []byte) {
				smc.NewPersonalReader(card, respCmd).ReadCID()
			},
			wantGet: "00c000000d",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			card := &capturingCard{atr: atr}
			tt.read(card, tt.respCmd)

			if len(card.cmds) != 2 {
				t.Fatalf("got %d exchanges, want the field command and its GET RESPONSE", len(card.cmds))
			}
			if got := hex.EncodeToString(card.cmds[1]); got != tt.wantGet {
				t.Errorf("GET RESPONSE = %s, want %s", got, tt.wantGet)
			}
		})
	}
}

// cannedCard answers each Transmit with the next response from a fixed
// script, so a test can model a card that misbehaves in one specific way —
// which a trace cannot: strict replay ties every response to the command that
// would have to be sent to reach it. Like capturingCard, it records the
// commands it was handed so a test can assert on the sequence.
type cannedCard struct {
	rsp  [][]byte
	cmds [][]byte
}

func (c *cannedCard) Status() (transport.Status, error) {
	return transport.Status{}, nil
}

func (c *cannedCard) Transmit(cmd []byte) ([]byte, error) {
	c.cmds = append(c.cmds, append([]byte(nil), cmd...))
	if len(c.cmds) > len(c.rsp) {
		return nil, fmt.Errorf("cannedCard: no scripted response for command %s", hex.EncodeToString(cmd))
	}
	return append([]byte(nil), c.rsp[len(c.cmds)-1]...), nil
}

func (c *cannedCard) Disconnect() error { return nil }

// TestReadFailedCommandSkipsGetResponse pins the first response of a pair: a
// field command answered with an error status must fail the field without a
// GET RESPONSE being sent at it, and the next field must read normally.
func TestReadFailedCommandSkipsGetResponse(t *testing.T) {
	card := &cannedCard{rsp: [][]byte{
		{0x6a, 0x82}, // the CID command fails: file not found
		{0x61, 0x0f}, // the Thai name command: 15 bytes ready
		append(append([]byte(nil), mustDecodeHex(payloadNameThai)...), 0x90, 0x00),
	}}

	reader := smc.NewPersonalReader(card, nil)
	if cid := reader.ReadCID(); cid != "" {
		t.Errorf("Cid = %q, want empty for a failed field command", cid)
	}
	if name := reader.ReadRawName(); name != "นาย#สมชาย##ใจดี" {
		t.Errorf("the field after the failed one should still read, got %q", name)
	}

	if len(card.cmds) != 3 {
		t.Fatalf("got %d exchanges, want the CID command, the Thai name command and one GET RESPONSE", len(card.cmds))
	}
	if !bytes.Equal(card.cmds[1], apdu.PersonalCMD.NameThai) {
		t.Errorf("second command = %s, want the Thai name command: a GET RESPONSE was sent at the failed command", hex.EncodeToString(card.cmds[1]))
	}
}

// TestReadFailingGetResponseFailsTheField covers the other half of the pair: a
// GET RESPONSE that answers with an error status fails the field, rather than
// coming back as empty or silently truncated data.
func TestReadFailingGetResponseFailsTheField(t *testing.T) {
	card := &cannedCard{rsp: [][]byte{
		{0x61, 0x0d}, // the CID command: 13 bytes ready
		{0x6a, 0x82}, // the GET RESPONSE fails
	}}

	reader := smc.NewPersonalReader(card, nil)
	if cid := reader.ReadCID(); cid != "" {
		t.Errorf("Cid = %q, want empty when GET RESPONSE fails", cid)
	}

	if len(card.cmds) != 2 {
		t.Fatalf("got %d exchanges, want the CID command and its GET RESPONSE", len(card.cmds))
	}
	if !bytes.HasPrefix(card.cmds[1], mustDecodeHex(getResponsePrefix)) {
		t.Errorf("second command = %s, want a GET RESPONSE", hex.EncodeToString(card.cmds[1]))
	}
}

func TestListReaders(t *testing.T) {
	reader, _ := newTestReader(t, personalTrace(false))

	readers, err := reader.ListReaders()
	if err != nil {
		t.Fatalf("ListReaders: %v", err)
	}
	if len(readers) != 1 || readers[0] != "Fake Reader 0" {
		t.Errorf("ListReaders = %v", readers)
	}
}

func TestReadWithExplicitReader(t *testing.T) {
	reader, _ := newTestReader(t, personalTrace(false))

	name := "Fake Reader 0"
	if _, err := reader.Read(&name, &smc.Options{}); err != nil {
		t.Fatalf("Read: %v", err)
	}
}

func TestReadWithNoTransport(t *testing.T) {
	reader := smc.NewSmartCardWith(nil)

	if _, err := reader.Read(nil, nil); err == nil {
		t.Error("expected an error when no transport is configured")
	}
	if _, err := reader.ListReaders(); err == nil {
		t.Error("expected an error from ListReaders with no transport")
	}
	if err := reader.StartDaemon(nil, nil); err == nil {
		t.Error("expected an error from StartDaemon with no transport")
	}
	if err := reader.Close(); err != nil {
		t.Errorf("Close with no transport should be a no-op, got %v", err)
	}
}

func TestStartDaemonCtxBroadcastsAndStops(t *testing.T) {
	reader, card := newTestReader(t, laserTrace())

	broadcast := make(chan model.Message, 16)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- reader.StartDaemonCtx(ctx, broadcast, &smc.Options{ShowLaserData: true})
	}()

	deadline := time.After(10 * time.Second)
	seen := map[string]bool{}
	for !seen["smc-removed"] {
		select {
		case msg := <-broadcast:
			seen[msg.Event] = true
		case err := <-done:
			t.Fatalf("daemon returned early: %v", err)
		case <-deadline:
			t.Fatalf("timed out, saw only %v", seen)
		}
	}

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("daemon returned %v, want context.Canceled", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("daemon did not stop after cancel")
	}

	for _, want := range []string{"smc-inserted", "smc-data", "smc-removed"} {
		if !seen[want] {
			t.Errorf("never saw %s event", want)
		}
	}
	if !card.Disconnected() {
		t.Error("the daemon should disconnect the card after removal")
	}
}

// recordedTracePath is where cmd/record writes. The file holds real personal
// data, so it is deliberately not committed.
var recordedTracePath = filepath.Join("..", "..", "testdata", "trace-real.json")

// traceHasApplet reports whether the capture contains the SELECT for an applet.
// cmd/record only reads an applet when its flag is passed, so a capture may
// cover personal data alone, or also the laser code and the NHSO record.
//
// Replay has to request the same applets the capture covered. The fake card
// replays strictly in order, so asking for an applet the capture skipped, or
// skipping one it recorded, makes the sequence drift and the read fail.
func traceHasApplet(trace *transport.Trace, selectCmd []byte) bool {
	want := hex.EncodeToString(selectCmd)
	for _, e := range trace.Exchanges {
		if e.Command == want {
			return true
		}
	}
	return false
}

// isThaiID reports whether s is 13 ASCII digits, the fixed width the CID
// command asks for. A read that lost or shifted a byte fails here instead of
// returning a shorter number that still looks plausible.
func isThaiID(s string) bool {
	return len(s) == 13 && asciiDigits(s)
}

// isLaserId reports whether s is two uppercase letters and ten digits, the
// shape the recorded card sends inside its 0x10 byte GET RESPONSE once the NUL
// padding is trimmed.
func isLaserId(s string) bool {
	if len(s) != 12 {
		return false
	}
	for i := 0; i < 2; i++ {
		if s[i] < 'A' || s[i] > 'Z' {
			return false
		}
	}
	return asciiDigits(s[2:])
}

// asciiDigits reports whether s is nothing but ASCII digits.
func asciiDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// TestReadFromRecordedTrace runs against a trace captured from a real card once
// one exists. It asserts structure rather than exact values, because the data
// is somebody's identity — with two shape exceptions: the CID is pinned to 13
// digits and the laser code to two letters and ten digits, so a card (or a
// read) that truncates or misaligns a fixed width field fails here instead of
// handing back quietly wrong data.
func TestReadFromRecordedTrace(t *testing.T) {
	if _, err := os.Stat(recordedTracePath); err != nil {
		t.Skipf("no recorded trace yet; capture one with: go run ./cmd/record -out %s", recordedTracePath)
	}

	trace, err := transport.LoadTrace(recordedTracePath)
	if err != nil {
		t.Fatalf("LoadTrace: %v", err)
	}
	reader, _ := newTestReader(t, trace)

	withLaser := traceHasApplet(trace, apdu.CardCMD.Select)
	withNhso := traceHasApplet(trace, apdu.NhsoCMD.Select)
	data, err := reader.Read(nil, &smc.Options{
		ShowFaceImage: true,
		ShowLaserData: withLaser,
		ShowNhsoData:  withNhso,
	})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if data == nil || data.Personal == nil {
		t.Fatal("no personal data returned")
	}
	if data.Personal.Cid == "" {
		t.Error("Cid is empty")
	} else if !isThaiID(data.Personal.Cid) {
		t.Errorf("Cid = %q, want 13 digits", data.Personal.Cid)
	}
	if data.Personal.Name.FirstName == "" {
		t.Error("first name is empty")
	}
	if data.Personal.Address.Province == "" {
		t.Error("province is empty")
	}
	// Decode the portrait rather than only checking that it is non-empty: a
	// portrait that lost a byte is still a non-empty string. The recorded card
	// pads the last chunk with 0x20, so a read that is merely too eager about
	// trimming shows up as a missing EOI marker here.
	if data.Personal.FaceImage == "" {
		t.Error("face image is empty")
	} else if raw, err := base64.StdEncoding.DecodeString(data.Personal.FaceImage); err != nil {
		t.Errorf("face image is not valid base64: %v", err)
	} else if len(raw) < 2 {
		t.Errorf("face image is %d bytes, too short to be a JPEG", len(raw))
	} else if !bytes.HasPrefix(raw, []byte{0xff, 0xd8}) {
		t.Errorf("face image starts with % x, want the JPEG SOI marker ff d8", raw[:2])
	} else if !bytes.HasSuffix(raw, []byte{0xff, 0xd9}) {
		t.Errorf("face image ends with % x, want the JPEG EOI marker ff d9", raw[len(raw)-2:])
	}

	if withLaser {
		if data.Card == nil {
			t.Fatal("Card is nil although the trace contains the laser applet")
		}
		if data.Card.LaserId == "" {
			t.Error("laser id is empty")
		} else if !isLaserId(data.Card.LaserId) {
			t.Errorf("LaserId = %q, want two letters and ten digits", data.Card.LaserId)
		}
	} else if data.Card != nil {
		t.Error("Card should be nil when the trace has no laser exchanges")
	}

	if !withNhso {
		if data.Nhso != nil {
			t.Error("Nhso should be nil when the trace has no NHSO exchanges")
		}
		t.Logf("trace %q: %d exchanges, laser=%v nhso=%v", trace.Name, len(trace.Exchanges), withLaser, withNhso)
		return
	}

	// The NHSO applet is optional on a card, so some of its fields are
	// legitimately blank. Assert on the ones the captured card fills in
	// rather than requiring the whole section.
	if data.Nhso == nil {
		t.Fatal("Nhso is nil although the trace contains the NHSO applet")
	}
	if data.Nhso.MainInscl == "" {
		t.Error("NHSO MainInscl is empty")
	}
	if data.Nhso.MainHospitalName == "" {
		t.Error("NHSO MainHospitalName is empty")
	}
	if data.Nhso.UpdateDate == "" {
		t.Error("NHSO UpdateDate is empty")
	}
	t.Logf("trace %q: %d exchanges, laser=%v nhso=%v, nhso hospital=%q",
		trace.Name, len(trace.Exchanges), withLaser, withNhso, data.Nhso.MainHospitalName)
}
