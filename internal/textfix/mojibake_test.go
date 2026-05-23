package textfix

import "testing"

func TestRepairMojibake(t *testing.T) {
	got := RepairMojibake("åŒ—äº¬å¸‚æµ·æ·€åŒºæµ‹è¯•è·¯ 1 å·")
	want := "北京市海淀区测试路 1 号"
	if got != want {
		t.Fatalf("RepairMojibake() = %q, want %q", got, want)
	}
}

func TestRepairMojibakeLeavesNormalText(t *testing.T) {
	for _, input := range []string{"北京市海淀区测试路 1 号", "Extra Alice", "ops@example.internal", "sk_live_demo"} {
		if got := RepairMojibake(input); got != input {
			t.Fatalf("RepairMojibake(%q) = %q", input, got)
		}
	}
}

func TestRepairStringGBK(t *testing.T) {
	got := RepairString("±±¾©ÊÐº£µíÇø", EncodingGBK)
	want := "北京市海淀区"
	if got != want {
		t.Fatalf("RepairString(GBK) = %q, want %q", got, want)
	}
}

func TestRepairBytesGBK(t *testing.T) {
	got := RepairBytes([]byte{0xb1, 0xb1, 0xbe, 0xa9, 0xca, 0xd0}, EncodingGBK)
	want := "北京市"
	if got != want {
		t.Fatalf("RepairBytes(GBK) = %q, want %q", got, want)
	}
}

func TestNormalizeEncoding(t *testing.T) {
	cases := map[string]string{
		"":            EncodingAuto,
		"UTF-8":       EncodingUTF8,
		"cp936":       EncodingGBK,
		"gb_18030":    EncodingGB18030,
		"sjis":        EncodingShiftJIS,
		"windows1252": EncodingWindows1252,
	}
	for input, want := range cases {
		if got := NormalizeEncoding(input); got != want {
			t.Fatalf("NormalizeEncoding(%q) = %q, want %q", input, got, want)
		}
	}
}
