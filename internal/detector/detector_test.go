package detector

import "testing"

func TestFieldKinds(t *testing.T) {
	kinds := FieldKinds("users", "mobile_phone")
	if len(kinds) != 2 || kinds[0] != Phone || kinds[1] != Username {
		t.Fatalf("unexpected kinds: %#v", kinds)
	}
}

func TestContentKinds(t *testing.T) {
	kinds := ContentKinds("张三 13800138000 test@example.com")
	seen := map[Kind]bool{}
	for _, k := range kinds {
		seen[k] = true
	}
	if !seen[Phone] || !seen[Email] {
		t.Fatalf("expected phone and email, got %#v", kinds)
	}
}

func TestMask(t *testing.T) {
	if got := Mask(Phone, "13800138000"); got != "138****8000" {
		t.Fatalf("unexpected phone mask: %s", got)
	}
	if got := Mask(IDCard, "11010119900101123X"); got != "110101********123X" {
		t.Fatalf("unexpected id mask: %s", got)
	}
}
