package smc

import (
	"encoding/hex"
	"fmt"

	"github.com/ebfe/scard"
	"github.com/somprasongd/go-thai-smartcard/pkg/apdu"
	"github.com/somprasongd/go-thai-smartcard/pkg/model"
	"github.com/somprasongd/go-thai-smartcard/pkg/util"
)

type personalReader struct {
	card    *scard.Card
	respCmd []byte
}

func NewPersonalReader(card *scard.Card, respCmd []byte) *personalReader {
	return &personalReader{
		card,
		respCmd,
	}
}

func (r *personalReader) Select() error {
	// Send command APDU
	_, err := r.card.Transmit(apdu.PersonalCMD.Select)
	return err
}

func (r *personalReader) Read(isReadFaceImage bool) *model.Personal {
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

func (r *personalReader) ReadCID() string {
	s, err := util.ReadData(r.card, apdu.PersonalCMD.Cid, r.respCmd)
	if err != nil {
		fmt.Println("Error Read CID:", err)
		return ""
	}
	return s
}

func (r *personalReader) ReadRawName() string {
	s, err := util.ReadDataThai(r.card, apdu.PersonalCMD.NameThai, r.respCmd)
	if err != nil {
		fmt.Println("Error Read Thai name:", err)
		return ""
	}
	return s
}

func (r *personalReader) ReadName() string {
	raw := r.ReadRawName()
	name := model.NewNameFromRaw(raw)

	return name.FullName
}

func (r *personalReader) ReadRawNameEng() string {
	s, err := util.ReadDataThai(r.card, apdu.PersonalCMD.NameEng, r.respCmd)
	if err != nil {
		fmt.Println("Error Read English name:", err)
		return ""
	}
	return s
}

func (r *personalReader) ReadNameEng() string {
	raw := r.ReadRawNameEng()
	name := model.NewNameFromRaw(raw)

	return name.FullName
}

func (r *personalReader) ReadDob() string {
	s, err := util.ReadData(r.card, apdu.PersonalCMD.Dob, r.respCmd)
	if err != nil {
		fmt.Println("Error Read Dob:", err)
		return ""
	}
	return string(model.NewFormatedDate(s))
}

func (r *personalReader) ReadGender() string {
	s, err := util.ReadData(r.card, apdu.PersonalCMD.Gender, r.respCmd)
	if err != nil {
		fmt.Println("Error Read Gender:", err)
		return ""
	}
	return s
}

func (r *personalReader) ReadCardIssuer() string {
	s, err := util.ReadDataThai(r.card, apdu.PersonalCMD.CardIssuer, r.respCmd)
	if err != nil {
		fmt.Println("Error Read CardIssuer:", err)
		return ""
	}
	return s
}

func (r *personalReader) ReadIssueDate() string {
	s, err := util.ReadData(r.card, apdu.PersonalCMD.IssueDate, r.respCmd)
	if err != nil {
		fmt.Println("Error Read IssueDate:", err)
		return ""
	}
	return string(model.NewFormatedDate(s))
}

func (r *personalReader) ReadExpireDate() string {
	s, err := util.ReadData(r.card, apdu.PersonalCMD.ExpireDate, r.respCmd)
	if err != nil {
		fmt.Println("Error Read ExpireDate:", err)
		return ""
	}
	return string(model.NewFormatedDate(s))
}

func (r *personalReader) ReadRawAddress() string {
	s, err := util.ReadDataThai(r.card, apdu.PersonalCMD.Address, r.respCmd)
	if err != nil {
		fmt.Println("Error Read Address:", err)
		return ""
	}
	return s
}

func (r *personalReader) ReadAddress() string {
	raw := r.ReadRawAddress()
	addr := model.NewAddressFromRaw(raw)

	return addr.Address
}

func (r *personalReader) ReadFaceImage() string {
	image := ""
	for _, v := range apdu.PersonalCMD.FaceImage {
		raw, err := util.ReadData(r.card, v, r.respCmd)
		if err != nil {
			fmt.Println("Error Read Face Image:", err)
			return ""
		}
		if len(raw) == 0 {
			break
		}
		hx := hex.EncodeToString([]byte(raw))
		image = image + hx
	}

	b := []byte(image)
	db, err := util.DecodeHex(b)
	if err != nil {
		fmt.Printf("failed to decode hex: %s", err)
		return ""
	}

	base64 := util.Base64Encode([]byte(db))
	return string(base64)
}
