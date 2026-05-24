package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/johannes-kuhfuss/fileupload-service/config"
)

func TestValidatorAcceptsValidToken(t *testing.T) {
	validator, key, issuer, audience := testValidator(t)

	token := signToken(t, key, map[string]any{
		"iss":       issuer,
		"aud":       audience,
		"sub":       "user-1",
		"tenant_id": "tenant-a",
		"scope":     ScopeUploadCreate + " " + ScopeUploadRead,
		"exp":       time.Now().Add(time.Hour).Unix(),
	})

	identity, err := validator.Validate(token)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if identity.Subject != "user-1" || identity.TenantID != "tenant-a" {
		t.Fatalf("identity = %+v", identity)
	}
	if !hasScope(identity, ScopeUploadCreate) || !hasScope(identity, ScopeUploadRead) {
		t.Fatalf("scopes = %#v", identity.Scopes)
	}
}

func TestValidatorFallsBackToPreferredUsernameWhenSubjectIsMissing(t *testing.T) {
	validator, key, issuer, audience := testValidator(t)

	token := signToken(t, key, map[string]any{
		"iss":                issuer,
		"aud":                audience,
		"preferred_username": "developer",
		"tenant_id":          "tenant-a",
		"scope":              ScopeUploadCreate,
		"exp":                time.Now().Add(time.Hour).Unix(),
	})

	identity, err := validator.Validate(token)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if identity.Subject != "developer" {
		t.Fatalf("subject = %q", identity.Subject)
	}
}

func TestValidatorRejectsInvalidTokens(t *testing.T) {
	validator, key, issuer, audience := testValidator(t)
	valid := map[string]any{
		"iss":       issuer,
		"aud":       audience,
		"sub":       "user-1",
		"tenant_id": "tenant-a",
		"scope":     ScopeUploadCreate,
		"exp":       time.Now().Add(time.Hour).Unix(),
	}

	tests := []struct {
		name   string
		claims map[string]any
	}{
		{name: "wrong issuer", claims: cloneClaims(valid, "iss", "wrong")},
		{name: "wrong audience", claims: cloneClaims(valid, "aud", "wrong")},
		{name: "expired", claims: cloneClaims(valid, "exp", time.Now().Add(-time.Hour).Unix())},
		{name: "missing tenant", claims: cloneClaims(valid, "tenant_id", "")},
		{name: "missing subject", claims: cloneClaims(valid, "sub", "")},
		{
			name: "missing subject fallback",
			claims: map[string]any{
				"iss":       issuer,
				"aud":       audience,
				"tenant_id": "tenant-a",
				"scope":     ScopeUploadCreate,
				"exp":       time.Now().Add(time.Hour).Unix(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := signToken(t, key, tt.claims)
			if _, err := validator.Validate(token); err == nil {
				t.Fatal("Validate() expected error")
			}
		})
	}
}

func TestRequireScopeMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	validator, key, issuer, audience := testValidator(t)

	router := gin.New()
	router.GET("/protected", validator.RequireScope(ScopeUploadCreate), func(c *gin.Context) {
		identity, ok := IdentityFromContext(c)
		if !ok {
			t.Fatal("identity missing from context")
		}
		c.JSON(http.StatusOK, gin.H{"tenant_id": identity.TenantID})
	})

	token := signToken(t, key, map[string]any{
		"iss":       issuer,
		"aud":       audience,
		"sub":       "user-1",
		"tenant_id": "tenant-a",
		"scope":     ScopeUploadCreate,
		"exp":       time.Now().Add(time.Hour).Unix(),
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/protected", nil)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("missing auth status = %d", w.Code)
	}

	token = signToken(t, key, map[string]any{
		"iss":       issuer,
		"aud":       audience,
		"sub":       "user-1",
		"tenant_id": "tenant-a",
		"scope":     ScopeUploadRead,
		"exp":       time.Now().Add(time.Hour).Unix(),
	})
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("wrong scope status = %d", w.Code)
	}
}

func testValidator(t *testing.T) (*Validator, *rsa.PrivateKey, string, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	issuer := "http://keycloak.local/realms/mam"
	audience := "fileupload-service"

	jwks := jwksResponse{
		Keys: []jwk{{
			Kty: "RSA",
			Kid: "test-key",
			Alg: "RS256",
			N:   base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()),
			E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.PublicKey.E)).Bytes()),
		}},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jwks)
	}))
	t.Cleanup(server.Close)

	var cfg config.AppConfig
	cfg.Auth.Issuer = issuer
	cfg.Auth.Audience = audience
	cfg.Auth.JWKSURL = server.URL
	validator, err := NewValidator(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewValidator() error = %v", err)
	}
	return validator, key, issuer, audience
}

func signToken(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	header := map[string]any{
		"alg": "RS256",
		"typ": "JWT",
		"kid": "test-key",
	}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("Marshal(header) error = %v", err)
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("Marshal(claims) error = %v", err)
	}
	signingInput := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(claimsJSON)
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("SignPKCS1v15() error = %v", err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func cloneClaims(input map[string]any, key string, value any) map[string]any {
	output := make(map[string]any, len(input))
	for k, v := range input {
		output[k] = v
	}
	output[key] = value
	return output
}
