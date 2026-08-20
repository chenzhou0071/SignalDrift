package lobby

import "testing"

func TestTokenRoundtrip(t *testing.T) {
	now := int64(1000)
	ti := NewTokenIssuer("secret", 60, func() int64 { return now })
	tok := ti.Issue(42)
	uid, ok := ti.Verify(tok)
	if !ok || uid != 42 {
		t.Fatalf("uid=%d ok=%v", uid, ok)
	}
}

func TestTokenExpired(t *testing.T) {
	now := int64(1000)
	ti := NewTokenIssuer("secret", 60, func() int64 { return now })
	tok := ti.Issue(42)
	now = 1061
	if _, ok := ti.Verify(tok); ok {
		t.Fatal("expired token must fail")
	}
}

func TestTokenExpiryBoundary(t *testing.T) {
	now := int64(1000)
	ti := NewTokenIssuer("secret", 60, func() int64 { return now })
	tok := ti.Issue(42)
	now = 1060 // 恰好等于 expiry：仍有效（严格大于才过期）
	if _, ok := ti.Verify(tok); !ok {
		t.Fatal("token must be valid at exact expiry")
	}
	now = 1061
	if _, ok := ti.Verify(tok); ok {
		t.Fatal("token must expire after expiry")
	}
}

func TestTokenMalformed(t *testing.T) {
	ti := NewTokenIssuer("secret", 60, func() int64 { return 1000 })
	tok := ti.Issue(42)
	bad := []string{
		"",
		"42",
		"42.",
		"42.1060",
		"a.b.c",
		"42.abc.def",
		"42.1060.",
		tok + ".extra",
	}
	for _, s := range bad {
		if uid, ok := ti.Verify(s); ok || uid != 0 {
			t.Fatalf("malformed %q must fail, got uid=%d ok=%v", s, uid, ok)
		}
	}
}

func TestTokenTampered(t *testing.T) {
	ti := NewTokenIssuer("secret", 60, func() int64 { return 1000 })
	tok := ti.Issue(42)
	bad := "9" + tok[1:]
	if _, ok := ti.Verify(bad); ok {
		t.Fatal("tampered token must fail")
	}
}
