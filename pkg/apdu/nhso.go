package apdu

type nhsoCommand struct {
	Select               []byte
	MainInscl            []byte
	SubInscl             []byte
	MainHospitalName     []byte
	SubHospitalName      []byte
	PaidType             []byte
	IssueDate            []byte
	ExpireDate           []byte
	UpdateDate           []byte
	ChangeHospitalAmount []byte
}

var NhsoCMD *nhsoCommand

func init() {
	NhsoCMD = &nhsoCommand{
		Select:               []byte{0x00, 0xa4, 0x04, 0x00, 0x08, 0xa0, 0x00, 0x00, 0x00, 0x54, 0x48, 0x00, 0x83},
		MainInscl:            []byte{0x80, 0xb0, 0x00, 0x04, 0x02, 0x00, 0x3c},
		SubInscl:             []byte{0x80, 0xb0, 0x00, 0x40, 0x02, 0x00, 0x64},
		MainHospitalName:     []byte{0x80, 0xb0, 0x00, 0xa4, 0x02, 0x00, 0x50},
		SubHospitalName:      []byte{0x80, 0xb0, 0x00, 0xf4, 0x02, 0x00, 0x50},
		PaidType:             []byte{0x80, 0xb0, 0x01, 0x44, 0x02, 0x00, 0x01},
		IssueDate:            []byte{0x80, 0xb0, 0x01, 0x45, 0x02, 0x00, 0x08},
		ExpireDate:           []byte{0x80, 0xb0, 0x01, 0x4d, 0x02, 0x00, 0x08},
		UpdateDate:           []byte{0x80, 0xb0, 0x01, 0x55, 0x02, 0x00, 0x08},
		ChangeHospitalAmount: []byte{0x80, 0xb0, 0x01, 0x5d, 0x02, 0x00, 0x01},
	}
}
