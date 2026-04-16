package credentials

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/go-github/v68/github"
	"golang.org/x/oauth2"
)

// InstallationToken exchanges a GitHub App JWT for a short-lived installation
// access token scoped to the given org.
//
// Flow:
//  1. Parse PEM → RSA private key
//  2. Sign JWT (iss=AppID, exp=10min) with RS256
//  3. GET /app/installations → find installation for org
//  4. POST /app/installations/{id}/access_tokens → get token (valid 1h)
func InstallationToken(ctx context.Context, appID int64, pemKey, org string) (string, error) {
	key, err := parseRSAKey(pemKey)
	if err != nil {
		return "", fmt.Errorf("parse PEM key: %w", err)
	}

	jwtToken, err := signJWT(appID, key)
	if err != nil {
		return "", fmt.Errorf("sign JWT: %w", err)
	}

	// Authenticate as the App using the JWT
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: jwtToken})
	appClient := github.NewClient(oauth2.NewClient(ctx, ts))

	// Find the installation for the target org
	installations, _, err := appClient.Apps.ListInstallations(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("list installations: %w", err)
	}

	var installID int64
	for _, inst := range installations {
		if inst.GetAccount().GetLogin() == org {
			installID = inst.GetID()
			break
		}
	}
	if installID == 0 {
		return "", fmt.Errorf("GitHub App not installed in org %q", org)
	}

	// Exchange for installation access token
	tok, _, err := appClient.Apps.CreateInstallationToken(ctx, installID, nil)
	if err != nil {
		return "", fmt.Errorf("create installation token: %w", err)
	}

	return tok.GetToken(), nil
}

// signJWT creates a signed RS256 JWT for GitHub App authentication.
// Valid for 10 minutes (GitHub maximum is 10 minutes).
func signJWT(appID int64, key *rsa.PrivateKey) (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(now.Add(-60 * time.Second)), // 60s clock skew buffer
		ExpiresAt: jwt.NewNumericDate(now.Add(9 * time.Minute)),
		Issuer:    fmt.Sprintf("%d", appID),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	return token.SignedString(key)
}

// parseRSAKey decodes a PEM-encoded RSA private key.
func parseRSAKey(pemData string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemData))
	if block == nil {
		return nil, fmt.Errorf("no PEM block found")
	}

	// Try PKCS#1 first (GitHub App keys are PKCS#1)
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}

	// Fall back to PKCS#8
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("PEM key is not RSA")
	}
	return rsaKey, nil
}
