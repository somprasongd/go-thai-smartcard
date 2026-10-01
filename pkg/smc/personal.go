package smc

import (
	"encoding/hex"
	"fmt"
	"log"

	"github.com/somprasongd/go-thai-smartcard/pkg/apdu"
	"github.com/somprasongd/go-thai-smartcard/pkg/model"
	"github.com/somprasongd/go-thai-smartcard/pkg/transport"
	"github.com/somprasongd/go-thai-smartcard/pkg/util"
)

// PersonalReader reads the personal data applet.
type PersonalReader struct {
	*reader
}

// NewPersonalReader returns a reader for the personal data applet.
func NewPersonalReader(card transport.Card, respCmd []byte) *PersonalReader {
	return &PersonalReader{newReader(card, respCmd)}
}

// Select selects the personal data applet.
func (r *PersonalReader) Select() error {
	_, err := r.card.Transmit(apdu.PersonalCMD.Select)
	return err
}

// Read reads the personal record, including the face image when asked for.
func (r *PersonalReader) Read(isReadFaceImage bool) *model.Personal {
	m := model.Personal{}
	m.Cid = r.ReadCID()
	m.Name = model.NewNameFromRaw(r.ReadRawName())
	m.NameEng = model.NewNameFromRaw(r.ReadRawNameEng())
	m.Dob = model.FormatedDate(r.ReadDob())
	m.Gender = r.ReadGender()
	m.CardIssuer = r.ReadCardIssuer()
	m.IssueDate = model.FormatedDate(r.ReadIssueDate())
	m.ExpireDate = model.FormatedDate(r.ReadExpireDate())
	m.Address = model.NewAddressFromRaw(r.ReadRawAddress())
	if isReadFaceImage {
		m.FaceImage = r.ReadFaceImage()
	}
	return &m
}

func (r *PersonalReader) ReadCID() string {
	return r.readField("CID", apdu.PersonalCMD.Cid, false)
}

func (r *PersonalReader) ReadRawName() string {
	return r.readField("Thai name", apdu.PersonalCMD.NameThai, true)
}

func (r *PersonalReader) ReadName() string {
	return model.NewNameFromRaw(r.ReadRawName()).FullName
}

func (r *PersonalReader) ReadRawNameEng() string {
	return r.readField("English name", apdu.PersonalCMD.NameEng, true)
}

func (r *PersonalReader) ReadNameEng() string {
	return model.NewNameFromRaw(r.ReadRawNameEng()).FullName
}

func (r *PersonalReader) ReadDob() string {
	s := r.readField("Dob", apdu.PersonalCMD.Dob, false)
	return string(model.NewFormatedDate(s))
}

func (r *PersonalReader) ReadGender() string {
	return r.readField("Gender", apdu.PersonalCMD.Gender, false)
}

func (r *PersonalReader) ReadCardIssuer() string {
	return r.readField("CardIssuer", apdu.PersonalCMD.CardIssuer, true)
}

func (r *PersonalReader) ReadIssueDate() string {
	s := r.readField("IssueDate", apdu.PersonalCMD.IssueDate, false)
	return string(model.NewFormatedDate(s))
}

func (r *PersonalReader) ReadExpireDate() string {
	s := r.readField("ExpireDate", apdu.PersonalCMD.ExpireDate, false)
	return string(model.NewFormatedDate(s))
}

func (r *PersonalReader) ReadRawAddress() string {
	return r.readField("Address", apdu.PersonalCMD.Address, true)
}

func (r *PersonalReader) ReadAddress() string {
	raw := r.ReadRawAddress()
	if raw == "" {
		log.Println("Cannot read address")
		return ""
	}
	return model.NewAddressFromRaw(raw).Address
}

// ReadFaceImage reads the portrait in chunks and returns it as base64.
func (r *PersonalReader) ReadFaceImage() string {
	image := ""
	for _, v := range apdu.PersonalCMD.FaceImage {
		raw, err := r.readData(v)
		if err != nil {
			log.Println("Error Read Face Image:", err)
			return ""
		}
		if len(raw) == 0 {
			break
		}
		image += hex.EncodeToString([]byte(raw))
	}

	db, err := util.DecodeHex([]byte(image))
	if err != nil {
		log.Printf("failed to decode hex: %s", err)
		return ""
	}
	return string(util.Base64Encode(db))
}

// readField reads one field and logs, rather than propagating, the error: a
// single unreadable field should not abort the whole record.
func (r *PersonalReader) readField(name string, cmd []byte, isThai bool) string {
	var (
		s   string
		err error
	)
	if isThai {
		s, err = r.readDataThai(cmd)
	} else {
		s, err = r.readData(cmd)
	}
	if err != nil {
		log.Println(fmt.Sprintf("Error Read %s: %v", name, err))
		return ""
	}
	return s
}
