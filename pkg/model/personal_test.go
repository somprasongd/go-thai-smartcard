package model

import "testing"

func TestNewNameFromRaw(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want Name
	}{
		{
			name: "no middle name",
			raw:  "นาย#สมชาย##ใจดี",
			want: Name{
				Prefix:    "นาย",
				FirstName: "สมชาย",
				LastName:  "ใจดี",
				FullName:  "นายสมชาย ใจดี",
			},
		},
		{
			name: "with middle name",
			raw:  "นาง#สมหญิง#ใจ#ดี",
			want: Name{
				Prefix:     "นาง",
				FirstName:  "สมหญิง",
				MiddleName: "ใจ",
				LastName:   "ดี",
				FullName:   "นางสมหญิง ใจ ดี",
			},
		},
		{
			name: "empty input yields an empty name instead of panicking",
			raw:  "",
			want: Name{},
		},
		{
			name: "truncated input yields an empty name instead of panicking",
			raw:  "นาย#สมชาย",
			want: Name{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewNameFromRaw(tt.raw)
			if got != tt.want {
				t.Errorf("NewNameFromRaw(%q) = %+v, want %+v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestNewAddressFromRaw(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want Address
	}{
		{
			name: "moo",
			raw:  "123/45#หมู่ที่ 4#ถนนสีหลาง 12#ตำบลคลองตาหลัง#อำเภอคลองตาหลัง#จังหวัดกรุงเทพมหานคร",
			want: Address{
				HouseNo:     "123/45",
				Moo:         "4",
				Street:      "ถนนสีหลาง 12",
				Subdistrict: "คลองตาหลัง",
				District:    "คลองตาหลัง",
				Province:    "กรุงเทพมหานคร",
				Address:     "123/45 หมู่ที่ 4 ถนนสีหลาง 12 ตำบลคลองตาหลัง อำเภอคลองตาหลัง จังหวัดกรุงเทพมหานคร",
			},
		},
		{
			name: "soi",
			raw:  "99#ซอยสุขใจ#ถนนสีหลาง 12#ตำบลคลองตาหลัง#อำเภอคลองตาหลัง#จังหวัดกรุงเทพมหานคร",
			want: Address{
				HouseNo:     "99",
				Soi:         "สุขใจ",
				Street:      "ถนนสีหลาง 12",
				Subdistrict: "คลองตาหลัง",
				District:    "คลองตาหลัง",
				Province:    "กรุงเทพมหานคร",
				Address:     "99 ซอยสุขใจ ถนนสีหลาง 12 ตำบลคลองตาหลัง อำเภอคลองตาหลัง จังหวัดกรุงเทพมหานคร",
			},
		},
		{
			name: "empty input yields an empty address instead of panicking",
			raw:  "",
			want: Address{},
		},
		{
			name: "too few fields yields an empty address instead of panicking",
			raw:  "123/45#หมู่ที่ 4",
			want: Address{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewAddressFromRaw(tt.raw)
			if got != tt.want {
				t.Errorf("NewAddressFromRaw(%q) = %+v, want %+v", tt.raw, got, tt.want)
			}
		})
	}
}

// A moi and a soi in the same address cannot both be populated: the parser
// looks for both prefixes in the same field. This records the current
// behaviour so that a later fix is a deliberate change.
func TestNewAddressFromRawMooWinsOverSoi(t *testing.T) {
	got := NewAddressFromRaw("1#หมู่ที่ 4#ซอยสุขใจ#ถนน#ตำบลคลองตาหลัง#อำเภอคลองตาหลัง#จังหวัดกรุงเทพมหานคร")
	if got.Moo != "4" {
		t.Errorf("Moo = %q, want %q", got.Moo, "4")
	}
	if got.Soi != "" {
		t.Errorf("Soi = %q, want empty", got.Soi)
	}
}

func TestNewFormatedDate(t *testing.T) {
	tests := []struct {
		raw  string
		want FormatedDate
	}{
		{raw: "25371231", want: "1994-12-31"},
		{raw: "25620515", want: "2019-05-15"},
		{raw: "25690515", want: "2026-05-15"},
		{raw: "", want: ""},
		{raw: "123", want: ""},
		{raw: "abcd1231", want: ""},
	}

	for _, tt := range tests {
		if got := NewFormatedDate(tt.raw); got != tt.want {
			t.Errorf("NewFormatedDate(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}
