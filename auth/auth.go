package auth

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/johannes-kuhfuss/fileupload-service/config"
	"github.com/johannes-kuhfuss/fileupload-service/domain"
)

const (
	ScopeUploadCreate   = "media:upload:create"
	ScopeUploadWrite    = "media:upload:write"
	ScopeUploadRead     = "media:upload:read"
	ScopeUploadComplete = "media:upload:complete"
)

const identityKey = "identity"

type Validator struct {
	issuer   string
	audience string
	keys     map[string]*rsa.PublicKey
	now      func() time.Time
}

type jwksResponse struct {
	Keys []jwk `json:"keys"`
}

type jwk struct {
	Kty string `json:"kty"`
	Use string `json:"use,omitempty"`
	Kid string `json:"kid"`
	Alg string `json:"alg,omitempty"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type tokenHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	Typ string `json:"typ,omitempty"`
}

type claims struct {
	Issuer            string          `json:"iss"`
	Subject           string          `json:"sub"`
	PreferredUsername string          `json:"preferred_username,omitempty"`
	Email             string          `json:"email,omitempty"`
	Audience          json.RawMessage `json:"aud"`
	ExpiresAt         int64           `json:"exp"`
	NotBefore         int64           `json:"nbf,omitempty"`
	IssuedAt          int64           `json:"iat,omitempty"`
	Scope             string          `json:"scope,omitempty"`
	Scopes            []string        `json:"scopes,omitempty"`
	TenantID          string          `json:"tenant_id"`
}

func NewValidator(ctx context.Context, cfg config.AppConfig) (*Validator, error) {
	if strings.TrimSpace(cfg.Auth.Issuer) == "" {
		return nil, errors.New("AUTH_ISSUER is required")
	}
	if strings.TrimSpace(cfg.Auth.Audience) == "" {
		return nil, errors.New("AUTH_AUDIENCE is required")
	}
	if strings.TrimSpace(cfg.Auth.JWKSURL) == "" {
		return nil, errors.New("AUTH_JWKS_URL is required")
	}

	keys, err := fetchJWKS(ctx, cfg.Auth.JWKSURL)
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return nil, errors.New("jwks did not contain any RSA keys")
	}
	return &Validator{
		issuer:   cfg.Auth.Issuer,
		audience: cfg.Auth.Audience,
		keys:     keys,
		now:      time.Now,
	}, nil
}

func (v *Validator) RequireScope(scope string) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := bearerToken(c.GetHeader("Authorization"))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": err.Error()})
			return
		}

		identity, err := v.Validate(token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": err.Error()})
			return
		}
		if !hasScope(identity, scope) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"message": "missing required scope"})
			return
		}

		SetIdentity(c, identity)
		c.Next()
	}
}

func SetIdentity(c *gin.Context, identity domain.Identity) {
	c.Set(identityKey, identity)
}

func IdentityFromContext(c *gin.Context) (domain.Identity, bool) {
	value, ok := c.Get(identityKey)
	if !ok {
		return domain.Identity{}, false
	}
	identity, ok := value.(domain.Identity)
	return identity, ok
}

func (v *Validator) Validate(token string) (domain.Identity, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return domain.Identity{}, errors.New("invalid bearer token")
	}

	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return domain.Identity{}, errors.New("invalid token header")
	}
	var header tokenHeader
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return domain.Identity{}, errors.New("invalid token header")
	}
	if header.Alg != "RS256" {
		return domain.Identity{}, errors.New("unsupported token algorithm")
	}
	key, ok := v.keys[header.Kid]
	if !ok {
		return domain.Identity{}, errors.New("token key not found")
	}

	signingInput := parts[0] + "." + parts[1]
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return domain.Identity{}, errors.New("invalid token signature")
	}
	digest := sha256.Sum256([]byte(signingInput))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature); err != nil {
		return domain.Identity{}, errors.New("invalid token signature")
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return domain.Identity{}, errors.New("invalid token payload")
	}
	var c claims
	if err := json.Unmarshal(payloadBytes, &c); err != nil {
		return domain.Identity{}, errors.New("invalid token payload")
	}
	if c.Issuer != v.issuer {
		return domain.Identity{}, errors.New("invalid token issuer")
	}
	if !audienceContains(c.Audience, v.audience) {
		return domain.Identity{}, errors.New("invalid token audience")
	}
	now := v.now().Unix()
	if c.ExpiresAt <= now {
		return domain.Identity{}, errors.New("token is expired")
	}
	if c.NotBefore != 0 && c.NotBefore > now {
		return domain.Identity{}, errors.New("token is not active yet")
	}
	subject := claimSubject(c)
	if subject == "" {
		return domain.Identity{}, errors.New("token subject is required")
	}
	if strings.TrimSpace(c.TenantID) == "" {
		return domain.Identity{}, errors.New("tenant_id claim is required")
	}

	return domain.Identity{
		Subject:  subject,
		TenantID: c.TenantID,
		Scopes:   claimScopes(c),
	}, nil
}

func fetchJWKS(ctx context.Context, jwksURL string) (map[string]*rsa.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch jwks: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch jwks: unexpected status %s", resp.Status)
	}

	var body jwksResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode jwks: %w", err)
	}
	keys := make(map[string]*rsa.PublicKey, len(body.Keys))
	for _, key := range body.Keys {
		if key.Kty != "RSA" || key.Kid == "" || key.N == "" || key.E == "" {
			continue
		}
		publicKey, err := rsaPublicKey(key)
		if err != nil {
			return nil, err
		}
		keys[key.Kid] = publicKey
	}
	return keys, nil
}

func rsaPublicKey(key jwk) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(key.N)
	if err != nil {
		return nil, fmt.Errorf("decode jwk modulus: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(key.E)
	if err != nil {
		return nil, fmt.Errorf("decode jwk exponent: %w", err)
	}
	exponent := 0
	for _, b := range eBytes {
		exponent = exponent<<8 + int(b)
	}
	if exponent == 0 {
		return nil, errors.New("invalid jwk exponent")
	}
	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: exponent,
	}, nil
}

func bearerToken(header string) (string, error) {
	if strings.TrimSpace(header) == "" {
		return "", errors.New("authorization header is required")
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
		return "", errors.New("authorization header must be a bearer token")
	}
	return strings.TrimSpace(parts[1]), nil
}

func hasScope(identity domain.Identity, scope string) bool {
	for _, candidate := range identity.Scopes {
		if candidate == scope {
			return true
		}
	}
	return false
}

func audienceContains(raw json.RawMessage, audience string) bool {
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		return single == audience
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err != nil {
		return false
	}
	for _, item := range many {
		if item == audience {
			return true
		}
	}
	return false
}

func claimScopes(c claims) []string {
	seen := map[string]bool{}
	var scopes []string
	for _, scope := range strings.Fields(c.Scope) {
		if !seen[scope] {
			scopes = append(scopes, scope)
			seen[scope] = true
		}
	}
	for _, scope := range c.Scopes {
		if !seen[scope] {
			scopes = append(scopes, scope)
			seen[scope] = true
		}
	}
	return scopes
}

func claimSubject(c claims) string {
	if subject := strings.TrimSpace(c.Subject); subject != "" {
		return subject
	}
	if username := strings.TrimSpace(c.PreferredUsername); username != "" {
		return username
	}
	return strings.TrimSpace(c.Email)
}
