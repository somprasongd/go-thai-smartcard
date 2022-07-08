package model

type Data struct {
	Personal *Personal `json:"personal"`
	Nhso     *Nhso     `json:"nhso,omitempty"`
}
