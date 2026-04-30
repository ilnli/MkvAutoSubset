package sfnt

import "testing"

func TestStringifyMacintoshTraditionalChinese(t *testing.T) {
	b := []byte{
		0xb5, 0xd8, 0xb1, 0x64, 0xae, 0xfc, 0xb3, 0xf8, 0xc5, 0xe9,
		0x57, 0x31, 0x32, 0x28, 0x50, 0x29,
	}

	got, err := stringifyMacintoshTraditionalChinese(b)
	if err != nil {
		t.Fatal(err)
	}

	const want = "\u83ef\u5eb7\u6d77\u5831\u9ad4W12(P)"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestStringifyMacintoshFallsBackToBig5(t *testing.T) {
	got, err := stringifyMacintosh([]byte{
		0xb5, 0xd8, 0xb1, 0x64, 0xae, 0xfc, 0xb3, 0xf8, 0xc5, 0xe9,
		0x57, 0x31, 0x32, 0x28, 0x50, 0x29,
	})
	if err != nil {
		t.Fatal(err)
	}

	const want = "\u83ef\u5eb7\u6d77\u5831\u9ad4W12(P)"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestEncodeBig5CmapCodepoint(t *testing.T) {
	got, ok := encodeBig5('\u83ef')
	if !ok {
		t.Fatal("Big5 encode failed")
	}

	const want = 0xb5d8
	if got != want {
		t.Fatalf("got %#x, want %#x", got, want)
	}
}
