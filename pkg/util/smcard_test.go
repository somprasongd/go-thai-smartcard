package util

import "testing"

func TestGetResponseCommand(t *testing.T) {
	tests := []struct {
		name string
		atr  []byte
		want []byte
	}{
		{
			// Observed on a real Thai national ID card through cmd/record.
			// Byte 1 is 0x79, so this card takes the legacy path.
			name: "real thai id card atr uses the legacy variant",
			atr:  []byte{0x3b, 0x79, 0x96, 0x00, 0x00, 0x54, 0x48, 0x20, 0x4e, 0x49, 0x44, 0x20, 0x31, 0x38},
			want: []byte{0x00, 0xc0, 0x00, 0x00},
		},
		{
			// Not observed on any card so far. Kept from the original
			// implementation; the test pins the behaviour, not the claim
			// that real cards answer this way.
			name: "3b 67 prefix selects the extended variant",
			atr:  []byte{0x3b, 0x67, 0x9e, 0x00, 0x00},
			want: []byte{0x00, 0xc0, 0x00, 0x01},
		},
		{
			name: "other atr uses the legacy variant",
			atr:  []byte{0x3b, 0x65, 0x00, 0x00},
			want: []byte{0x00, 0xc0, 0x00, 0x00},
		},
		{
			name: "truncated atr no longer panics",
			atr:  []byte{0x3b},
			want: []byte{0x00, 0xc0, 0x00, 0x00},
		},
		{
			name: "empty atr no longer panics",
			atr:  nil,
			want: []byte{0x00, 0xc0, 0x00, 0x00},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetResponseCommand(tt.atr)
			if len(got) != len(tt.want) {
				t.Fatalf("GetResponseCommand(%x) = %x, want %x", tt.atr, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("GetResponseCommand(%x) = %x, want %x", tt.atr, got, tt.want)
				}
			}
		})
	}
}

// The GET RESPONSE command is shared state, so appending onto it must not be
// able to corrupt it for the next reader. The returned slice is full
// (len == cap), so append is forced to copy.
func TestGetResponseCommandReturnsIndependentSlices(t *testing.T) {
	first := GetResponseCommand([]byte{0x3b, 0x67})
	appended := append(first[:len(first):len(first)], 0xff)
	if len(appended) != 5 || appended[4] != 0xff {
		t.Fatalf("append produced %x, want a 5 byte slice ending in ff", appended)
	}

	second := GetResponseCommand([]byte{0x3b, 0x67})
	if second[3] != 0x01 {
		t.Errorf("second call returned %x, want the extended variant unchanged", second)
	}
}
