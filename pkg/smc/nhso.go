package smc

import (
	"log"

	"github.com/ebfe/scard"
	"github.com/somprasongd/go-thai-smartcard/pkg/apdu"
	"github.com/somprasongd/go-thai-smartcard/pkg/model"
	"github.com/somprasongd/go-thai-smartcard/pkg/util"
)

type nhsoReader struct {
	card    *scard.Card
	respCmd []byte
}

func NewNhsoReader(card *scard.Card, respCmd []byte) *nhsoReader {
	return &nhsoReader{
		card,
		respCmd,
	}
}

func (r *nhsoReader) Select() error {
	// Send command APDU
	_, err := r.card.Transmit(apdu.NhsoCMD.Select)
	return err
}

func (r *nhsoReader) Read() *model.Nhso {
	m := model.Nhso{}
	m.MainInscl = r.ReadMainInscl()
	m.SubInscl = r.ReadSubInscl()
	m.MainHospitalName = r.ReadMainHospitalName()
	m.SubHospitalName = r.ReadSubHospitalName()
	m.PaidType = r.ReadPaidType()
	m.IssueDate = model.FormatedDate(r.ReadIssueDate())
	m.ExpireDate = model.FormatedDate(r.ReadExpireDate())
	m.UpdateDate = model.FormatedDate(r.ReadUpdateDate())
	m.ChangeHospitalAmount = r.ReadChangeHospitalAmount()
	return &m
}

func (r *nhsoReader) ReadMainInscl() string {
	s, err := util.ReadDataThai(r.card, apdu.NhsoCMD.MainInscl, r.respCmd)
	if err != nil {
		log.Println("Error Read MainInscl:", err)
		return ""
	}
	return s
}

func (r *nhsoReader) ReadSubInscl() string {
	s, err := util.ReadDataThai(r.card, apdu.NhsoCMD.SubInscl, r.respCmd)
	if err != nil {
		log.Println("Error Read SubInscl:", err)
		return ""
	}
	return s
}

func (r *nhsoReader) ReadMainHospitalName() string {
	s, err := util.ReadDataThai(r.card, apdu.NhsoCMD.MainHospitalName, r.respCmd)
	if err != nil {
		log.Println("Error Read MainHospitalName:", err)
		return ""
	}
	return s
}

func (r *nhsoReader) ReadSubHospitalName() string {
	s, err := util.ReadDataThai(r.card, apdu.NhsoCMD.SubHospitalName, r.respCmd)
	if err != nil {
		log.Println("Error Read SubHospitalName:", err)
		return ""
	}
	return s
}

func (r *nhsoReader) ReadPaidType() string {
	s, err := util.ReadDataThai(r.card, apdu.NhsoCMD.PaidType, r.respCmd)
	if err != nil {
		log.Println("Error Read PaidType:", err)
		return ""
	}
	return s
}

func (r *nhsoReader) ReadIssueDate() string {
	s, err := util.ReadData(r.card, apdu.NhsoCMD.IssueDate, r.respCmd)
	if err != nil {
		log.Println("Error Read IssueDate:", err)
		return ""
	}
	return string(model.NewFormatedDate(s))
}

func (r *nhsoReader) ReadExpireDate() string {
	s, err := util.ReadData(r.card, apdu.NhsoCMD.ExpireDate, r.respCmd)
	if err != nil {
		log.Println("Error Read ExpireDate:", err)
		return ""
	}
	return string(model.NewFormatedDate(s))
}

func (r *nhsoReader) ReadUpdateDate() string {
	s, err := util.ReadData(r.card, apdu.NhsoCMD.UpdateDate, r.respCmd)
	if err != nil {
		log.Println("Error Read UpdateDate:", err)
		return ""
	}
	return string(model.NewFormatedDate(s))
}

func (r *nhsoReader) ReadChangeHospitalAmount() string {
	s, err := util.ReadData(r.card, apdu.NhsoCMD.ChangeHospitalAmount, r.respCmd)
	if err != nil {
		log.Println("Error Read ChangeHospitalAmount:", err)
		return ""
	}
	return s
}
