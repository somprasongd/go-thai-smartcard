package model

type Nhso struct {
	MainInscl            string       `json:"main_inscl"`
	SubInscl             string       `json:"sub_inscl"`
	MainHospitalName     string       `json:"main_hospital"`
	SubHospitalName      string       `json:"sub_hospital"`
	PaidType             string       `json:"paid_type"`
	IssueDate            FormatedDate `json:"issue_date"`
	ExpireDate           FormatedDate `json:"expire_date"`
	UpdateDate           FormatedDate `json:"update_date"`
	ChangeHospitalAmount string       `json:"change_hospital_amount"`
}
