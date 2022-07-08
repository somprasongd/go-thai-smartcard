package model

import (
	"fmt"
	"strconv"
)

type FormatedDate string

func NewFormatedDate(raw string) FormatedDate {
	if len(raw) != 8 {
		return ""
	}
	thaiYear := raw[:4]
	year, err := strconv.Atoi(thaiYear)
	if err != nil {
		return ""
	}

	return FormatedDate(
		fmt.Sprintf("%v-%s-%s", year-543, raw[4:6], raw[6:]),
	)
}
