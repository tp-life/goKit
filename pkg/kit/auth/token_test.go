package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func testManager() *TokenManager {
	return NewTokenManager(Config{Secret: "test-secret", Issuer: "gokit", ExpireMinutes: 1})
}

func TestTokenRoundTrip(t *testing.T) {
	m := testManager()
	token, err := m.Generate(42, "alice", true)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	claims, err := m.Parse(token)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if claims.UserID != 42 || claims.Username != "alice" || !claims.IsSuper {
		t.Errorf("claims mismatch: %+v", claims)
	}
}

func TestParseRejectsBadInput(t *testing.T) {
	m := testManager()
	if _, err := m.Parse(""); err == nil {
		t.Error("empty token should fail")
	}
	if _, err := m.Parse("not-a-token"); err == nil {
		t.Error("garbage token should fail")
	}

	// 不同 secret 签发的令牌必须被拒绝
	other := NewTokenManager(Config{Secret: "other-secret"})
	token, _ := other.Generate(1, "bob", false)
	if _, err := m.Parse(token); err == nil {
		t.Error("token signed by another secret should fail")
	}
}

func TestParseRejectsExpired(t *testing.T) {
	m := testManager()
	// 直接构造一个已过期的令牌
	claims := Claims{
		UserID:   1,
		Username: "bob",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := m.Parse(token); err == nil {
		t.Error("expired token should fail")
	}
}

func TestCurrentUserContext(t *testing.T) {
	if FromContext(t.Context()) != nil {
		t.Error("empty ctx should return nil")
	}
	u := &CurrentUser{UserID: 9, Username: "c", IsSuper: false}
	got := FromContext(WithCurrentUser(t.Context(), u))
	if got != u {
		t.Errorf("got %+v, want %+v", got, u)
	}
}
