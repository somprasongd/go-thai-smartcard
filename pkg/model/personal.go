package model

import (
	"fmt"
	"strings"
)

type Personal struct {
	Cid        string       `json:"cid"`
	Name       Name         `json:"name"`
	NameEng    Name         `json:"name_eng"`
	Dob        FormatedDate `json:"dob"`
	Gender     string       `json:"gender"`
	CardIssuer string       `json:"card_issuer"`
	IssueDate  FormatedDate `json:"issue_date"`
	ExpireDate FormatedDate `json:"expire_date"`
	Address    Address      `json:"address"`
	FaceImage  string       `json:"face_image"`
}

type Name struct {
	Prefix     string `json:"prefix"`
	FirstName  string `json:"first_name"`
	MiddleName string `json:"middle_name"`
	LastName   string `json:"last_name"`
	FullName   string `json:"full_name"`
}

func NewNameFromRaw(raw string) Name {
	temps := strings.Split(raw, "#")
	n := Name{}
	n.Prefix = temps[0]
	n.FirstName = temps[1]
	n.MiddleName = temps[2]
	n.LastName = temps[3]
	if n.MiddleName == "" {
		n.FullName = fmt.Sprintf("%s%s %s", n.Prefix, n.FirstName, n.LastName)
	} else {
		n.FullName = fmt.Sprintf("%s%s %s %s", n.Prefix, n.FirstName, n.MiddleName, n.LastName)
	}
	return n
}

type Address struct {
	HouseNo     string `json:"house_no"`
	Moo         string `json:"moo"`
	Soi         string `json:"soi"`
	Street      string `json:"street"`
	Subdistrict string `json:"subdistrict"`
	District    string `json:"district"`
	Province    string `json:"province"`
	Address     string `json:"address"`
}

func NewAddressFromRaw(raw string) Address {
	temps := strings.Split(raw, "#")
	a := Address{}
	a.HouseNo = temps[0]

	if strings.HasPrefix(temps[1], "หมู่ที่") {
		a.Moo = strings.TrimSpace(strings.TrimPrefix(temps[1], "หมู่ที่"))
	}

	if strings.HasPrefix(temps[1], "ซอย") {
		a.Soi = strings.TrimSpace(strings.TrimPrefix(temps[1], "ซอย"))
	}

	a.Street = strings.TrimSpace(strings.Join(temps[2:len(temps)-3], " "))

	subdistrict := temps[len(temps)-3]
	if strings.HasPrefix(subdistrict, "ตำบล") {
		a.Subdistrict = strings.TrimSpace(strings.TrimPrefix(subdistrict, "ตำบล"))
	} else if strings.HasPrefix(subdistrict, "แขวง") {
		a.Subdistrict = strings.TrimSpace(strings.TrimPrefix(subdistrict, "แขวง"))
	} else {
		a.Subdistrict = subdistrict
	}

	district := temps[len(temps)-2]
	if strings.HasPrefix(district, "อำเภอ") {
		a.District = strings.TrimSpace(strings.TrimPrefix(district, "อำเภอ"))
	} else if strings.HasPrefix(district, "เขต") {
		a.District = strings.TrimSpace(strings.TrimPrefix(district, "เขต"))
	} else {
		a.District = district
	}

	province := temps[len(temps)-1]
	a.Province = strings.TrimSpace(strings.TrimPrefix(province, "จังหวัด"))

	for i, v := range temps {
		if len(v) == 0 {
			continue
		}
		if i == 0 {
			a.Address = v
			continue
		}
		a.Address = a.Address + " " + v
	}

	return a
}
