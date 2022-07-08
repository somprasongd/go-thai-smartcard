package model

type Personal struct {
	Cid        string  `json:"cid"`
	Name       Name    `json:"name"`
	NameEng    Name    `json:"name_eng"`
	Dob        string  `json:"dob"`
	Gender     string  `json:"gender"`
	CardIssuer string  `json:"card_issuer"`
	IssueDate  string  `json:"issue_date"`
	ExpireDate string  `json:"expire_date"`
	Address    Address `json:"address"`
	FaceImage  string  `json:"face_image"`
}

type Name struct {
	Prefix     string `json:"prefix"`
	FirstName  string `json:"first_name"`
	MiddleName string `json:"middle_name"`
	LastName   string `json:"last_name"`
	FullName   string `json:"full_name"`
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
