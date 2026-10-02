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

// The soi has a slot of its own on the card, so an address that carries both
// a moo and a soi must populate both. The old parser looked for both prefixes
// in the moo's slot and dropped the soi from every such address (issue #7).
func TestNewAddressFromRawKeepsMooAndSoi(t *testing.T) {
	got := NewAddressFromRaw("1#หมู่ที่ 4#ซอยสุขใจ#ถนน#ตำบลคลองตาหลัง#อำเภอคลองตาหลัง#จังหวัดกรุงเทพมหานคร")
	if got.Moo != "4" {
		t.Errorf("Moo = %q, want %q", got.Moo, "4")
	}
	if got.Soi != "สุขใจ" {
		t.Errorf("Soi = %q, want %q", got.Soi, "สุขใจ")
	}
	if got.Street != "ถนน" {
		t.Errorf("Street = %q, want %q", got.Street, "ถนน")
	}
	if got.Subdistrict != "คลองตาหลัง" || got.District != "คลองตาหลัง" || got.Province != "กรุงเทพมหานคร" {
		t.Errorf("trailing fields = %+v", got)
	}
}

// The layout comes from a real card (testdata/trace-real.json, decoded from
// TIS-620): eight '#'-separated slots — house number, moo, soi, two more
// middle slots that this address leaves empty, then the three trailing
// fields — with the whole field padded with trailing spaces. The soi and
// street stay empty, the moo is read from its own slot, and the padding does
// not reach any parsed value.
func TestNewAddressFromRawRealCardLayout(t *testing.T) {
	raw := "140/44#หมู่ที่ 7####ตำบลกะทู้#อำเภอกะทู้#จังหวัดภูเก็ต" +
		"                                              "

	got := NewAddressFromRaw(raw)
	want := Address{
		HouseNo:     "140/44",
		Moo:         "7",
		Subdistrict: "กะทู้",
		District:    "กะทู้",
		Province:    "ภูเก็ต",
		Address:     "140/44 หมู่ที่ 7 ตำบลกะทู้ อำเภอกะทู้ จังหวัดภูเก็ต",
	}
	if got != want {
		t.Errorf("NewAddressFromRaw(real layout) =\n%+v\nwant\n%+v", got, want)
	}
}

// An address can carry a soi without a moo: the soi is still found in its
// own slot, further along than the parser used to look.
func TestNewAddressFromRawSoiInItsOwnSlot(t *testing.T) {
	got := NewAddressFromRaw("99##ซอยสุขใจ##ถนนสีหลาง 12#ตำบลคลองตาหลัง#อำเภอคลองตาหลัง#จังหวัดกรุงเทพมหานคร")
	if got.Soi != "สุขใจ" {
		t.Errorf("Soi = %q, want %q", got.Soi, "สุขใจ")
	}
	if got.Moo != "" {
		t.Errorf("Moo = %q, want empty", got.Moo)
	}
	if got.Street != "ถนนสีหลาง 12" {
		t.Errorf("Street = %q, want %q", got.Street, "ถนนสีหลาง 12")
	}
}

// A middle slot without a label — a village name, for example — is address
// text and lands in the street, as it always has.
func TestNewAddressFromRawUnlabelledSlotJoinsTheStreet(t *testing.T) {
	got := NewAddressFromRaw("123/45#หมู่ที่ 4#บ้านวังก์ทอง 112#ตำบลกะทู้#อำเภอกะทู้#จังหวัดภูเก็ต")
	if got.Moo != "4" {
		t.Errorf("Moo = %q, want %q", got.Moo, "4")
	}
	if got.Street != "บ้านวังก์ทอง 112" {
		t.Errorf("Street = %q, want %q", got.Street, "บ้านวังก์ทอง 112")
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
