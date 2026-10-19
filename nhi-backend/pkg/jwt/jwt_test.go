package jwt

import (
	"strings"
	"testing"
	"time"
)

const testSecret = "0123456789abcdef0123456789abcdef"

func TestTokenCarriesSessionID(t *testing.T) {
	tok, err := GenerateToken("a@b.com", "11111111-1111-1111-1111-111111111111", "tenant_a", "admin", "22222222-2222-2222-2222-222222222222", testSecret, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := ValidateToken(tok, testSecret)
	if err != nil {
		t.Fatal(err)
	}
	if claims.ID != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("jti = %q", claims.ID)
	}
}

func TestGenerateTokenRequiresSessionID(t *testing.T) {
	_, err := GenerateToken("a@b.com", "t", "d", "r", " ", testSecret, time.Minute)
	if err == nil || !strings.Contains(err.Error(), "session") {
		t.Fatalf("expected session id error, got %v", err)
	}
}
