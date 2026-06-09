package detector

import "testing"

func TestFieldKinds(t *testing.T) {
	kinds := FieldKinds("users", "mobile_phone")
	if len(kinds) != 2 || kinds[0] != Phone || kinds[1] != Username {
		t.Fatalf("unexpected kinds: %#v", kinds)
	}
}

func TestFieldKindsByLevel(t *testing.T) {
	kinds := FieldKindsByLevel(LevelHigh, "users", "mobile_phone", "password_hash")
	if len(kinds) != 1 || kinds[0] != Password {
		t.Fatalf("unexpected high-level kinds: %#v", kinds)
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

func TestHaESensitiveContentKinds(t *testing.T) {
	cases := []struct {
		name string
		text string
		kind Kind
	}{
		{name: "jwt", text: "token=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.signature_demo", kind: JWT},
		{name: "cloud key", text: "access_key_secret=LTAI5tQH9v2DemoSecret12", kind: CloudKey},
		{name: "authorization header", text: "Authorization: Bearer abcdefghijklmnopqrstuvwxyz123456", kind: Auth},
		{name: "jdbc", text: "spring.datasource.url=jdbc:mysql://127.0.0.1:3306/app?user=root&password=pass", kind: JDBC},
		{name: "wecom", text: "corpsecret=ww1234567890abcdef", kind: WeComKey},
		{name: "sensitive field", text: "refresh_token=rt_live_1234567890", kind: Password},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seen := map[Kind]bool{}
			for _, kind := range ContentKindsByLevel(LevelHigh, tc.text) {
				seen[kind] = true
			}
			if !seen[tc.kind] {
				t.Fatalf("expected %s in %#v", tc.kind, seen)
			}
		})
	}
}

func TestContentKindsByLevel(t *testing.T) {
	kinds := ContentKindsByLevel(LevelMedium, "11010119900101123X 13800138000 test@example.com")
	seen := map[Kind]bool{}
	for _, k := range kinds {
		seen[k] = true
	}
	if seen[IDCard] || !seen[Phone] || !seen[Email] {
		t.Fatalf("unexpected medium-level kinds: %#v", kinds)
	}
}

func TestParseLevel(t *testing.T) {
	level, ok := ParseLevel("最敏感")
	if !ok || level != LevelHigh {
		t.Fatalf("unexpected parsed level: %q %v", level, ok)
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
