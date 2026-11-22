package checkin

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func signNestJSHS256Token(t *testing.T, secret string, sub, name, email, role string) string {
	t.Helper()
	now := time.Now().Unix()
	headerRaw, err := json.Marshal(map[string]string{
		"alg": "HS256",
		"typ": "JWT",
	})
	if err != nil {
		t.Fatal(err)
	}
	claimsRaw, err := json.Marshal(map[string]any{
		"sub":   sub,
		"name":  name,
		"email": email,
		"role":  role,
		"iat":   now,
		"exp":   now + 8*3600,
		"jti":   "interop-test-jti-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	signingInput := base64.RawURLEncoding.EncodeToString(headerRaw) + "." + base64.RawURLEncoding.EncodeToString(claimsRaw)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signingInput))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return signingInput + "." + signature
}

func TestNestJWTIsAcceptedByGoCheckin(t *testing.T) {
	jwt := signNestJSHS256Token(
		t,
		testSecret,
		"u-door-1",
		"Diego Mamani",
		"portero@entradasya.bo",
		"doorman",
	)
	svc := service(t, fake(), nil)
	response := call(t, svc, "POST", "/checkin", jwt, payload())
	if response.Code != http.StatusOK || decode(t, response)["result"] != "allowed" {
		t.Fatal(response.Code, response.Body.String())
	}

	wrongSecret := signNestJSHS256Token(
		t,
		"another-secret-with-at-least-32-characters",
		"u-door-1",
		"Diego Mamani",
		"portero@entradasya.bo",
		"doorman",
	)
	rejected := call(t, svc, "POST", "/checkin", wrongSecret, payload())
	if rejected.Code != http.StatusUnauthorized {
		t.Fatal(rejected.Code, rejected.Body.String())
	}
}
