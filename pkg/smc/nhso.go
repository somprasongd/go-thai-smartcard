package smc

import (
	"fmt"
	"log"

	"github.com/somprasongd/go-thai-smartcard/pkg/apdu"
	"github.com/somprasongd/go-thai-smartcard/pkg/model"
	"github.com/somprasongd/go-thai-smartcard/pkg/transport"
)

// NhsoReader reads the national health security applet.
type NhsoReader struct {
	*reader
}

// NewNhsoReader returns a reader for the NHSO applet.
func NewNhsoReader(card transport.Card, respCmd []byte) *NhsoReader {
	return &NhsoReader{newReader(card, respCmd)}
}

// Select selects the NHSO applet.
func (r *NhsoReader) Select() error {
	_, err := r.card.Transmit(apdu.NhsoCMD.Select)
	return err
}

// Read reads the NHSO record.
func (r *NhsoReader) Read() *model.Nhso {
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

func (r *NhsoReader) ReadMainInscl() string {
	return r.readField("MainInscl", apdu.NhsoCMD.MainInscl, true)
}

func (r *NhsoReader) ReadSubInscl() string {
	return r.readField("SubInscl", apdu.NhsoCMD.SubInscl, true)
}

func (r *NhsoReader) ReadMainHospitalName() string {
	return r.readField("MainHospitalName", apdu.NhsoCMD.MainHospitalName, true)
}

func (r *NhsoReader) ReadSubHospitalName() string {
	return r.readField("SubHospitalName", apdu.NhsoCMD.SubHospitalName, true)
}

func (r *NhsoReader) ReadPaidType() string {
	return r.readField("PaidType", apdu.NhsoCMD.PaidType, true)
}

func (r *NhsoReader) ReadIssueDate() string {
	s := r.readField("IssueDate", apdu.NhsoCMD.IssueDate, false)
	return string(model.NewFormatedDate(s))
}

func (r *NhsoReader) ReadExpireDate() string {
	s := r.readField("ExpireDate", apdu.NhsoCMD.ExpireDate, false)
	return string(model.NewFormatedDate(s))
}

func (r *NhsoReader) ReadUpdateDate() string {
	s := r.readField("UpdateDate", apdu.NhsoCMD.UpdateDate, false)
	return string(model.NewFormatedDate(s))
}

func (r *NhsoReader) ReadChangeHospitalAmount() string {
	return r.readField("ChangeHospitalAmount", apdu.NhsoCMD.ChangeHospitalAmount, false)
}

// readField reads one field and logs, rather than propagating, the error: a
// single unreadable field should not abort the whole record.
func (r *NhsoReader) readField(name string, cmd []byte, isThai bool) string {
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
