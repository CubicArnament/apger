package credentials

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	sodium "github.com/GoKillers/libsodium-go/cryptosign"
	"github.com/golang-jwt/jwt/v5"
)

// ValidatePGPKey validates that the key is a valid OpenPGP private key and uses ECC (not RSA).
func ValidatePGPKey(armoredKey string) error {
	if armoredKey == "" {
		return fmt.Errorf("PGP key is empty")
	}

	// Decode armored key
	block, err := armor.Decode(strings.NewReader(armoredKey))
	if err != nil {
		return fmt.Errorf("invalid armored PGP key: %w", err)
	}

	// Parse packet
	reader := packet.NewReader(block.Body)
	pkt, err := reader.Next()
	if err != nil {
		return fmt.Errorf("failed to read PGP packet: %w", err)
	}

	// Check if it's a private key
	privKey, ok := pkt.(*packet.PrivateKey)
	if !ok {
		return fmt.Errorf("not a private key packet")
	}

	// Check algorithm - reject RSA
	switch privKey.PubKeyAlgo {
	case packet.PubKeyAlgoRSA, packet.PubKeyAlgoRSAEncryptOnly, packet.PubKeyAlgoRSASignOnly:
		return fmt.Errorf("RSA Deprecated - введите ECC ключ")
	case packet.PubKeyAlgoECDSA, packet.PubKeyAlgoEdDSA, packet.PubKeyAlgoECDH:
		return nil // ECC key - OK
	default:
		return fmt.Errorf("unknown key algorithm: %v", privKey.PubKeyAlgo)
	}
}

// ValidateLibsodiumKey validates that the key is a valid libsodium Ed25519 private key.
// Accepts base64-encoded 64-byte seed or 32-byte seed.
func ValidateLibsodiumKey(key string) error {
	if key == "" {
		return fmt.Errorf("libsodium key is empty")
	}

	// Try to decode as base64
	decoded, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return fmt.Errorf("invalid base64 encoding: %w", err)
	}

	// Check length - Ed25519 seed is 32 bytes, or 64 bytes (seed+pubkey)
	if len(decoded) != 32 && len(decoded) != 64 {
		return fmt.Errorf("invalid key length: expected 32 or 64 bytes, got %d", len(decoded))
	}

	// Verify it's a valid Ed25519 key by trying to use it
	if len(decoded) == 32 {
		// 32-byte seed - try to generate keypair
		_, _, ret := sodium.CryptoSignSeedKeypair(decoded)
		if ret != 0 {
			return fmt.Errorf("invalid Ed25519 seed")
		}
	} else {
		// 64-byte key - validate format
		// First 32 bytes = seed, last 32 bytes = public key
		seed := decoded[:32]
		_, pubkey, ret := sodium.CryptoSignSeedKeypair(seed)
		if ret != 0 {
			return fmt.Errorf("invalid Ed25519 key")
		}
		// Verify public key matches
		if !bytes.Equal(pubkey, decoded[32:]) {
			return fmt.Errorf("public key mismatch in 64-byte key")
		}
	}

	return nil
}

// ValidateGitHubPAT validates a GitHub Personal Access Token by making a test API call.
func ValidateGitHubPAT(ctx context.Context, pat string) error {
	if pat == "" {
		return fmt.Errorf("PAT is empty")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/user", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+pat)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to validate PAT: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("invalid PAT: HTTP %d", resp.StatusCode)
	}

	return nil
}

// ValidateGitHubAppPEM validates a GitHub App PEM key by checking JWT generation.
func ValidateGitHubAppPEM(appID int64, pemKey string) error {
	if appID == 0 {
		return fmt.Errorf("App ID is 0")
	}
	if pemKey == "" {
		return fmt.Errorf("PEM key is empty")
	}

	// Try to parse PEM key
	key, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(pemKey))
	if err != nil {
		// Try ECDSA
		_, err2 := jwt.ParseECPrivateKeyFromPEM([]byte(pemKey))
		if err2 != nil {
			return fmt.Errorf("invalid PEM key: not RSA or ECDSA: %w", err)
		}
	}

	// Try to generate a JWT token
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iat": jwt.NewNumericDate(jwt.TimeFunc()),
		"exp": jwt.NewNumericDate(jwt.TimeFunc().Add(10 * 60)),
		"iss": fmt.Sprintf("%d", appID),
	})

	_, err = token.SignedString(key)
	if err != nil {
		return fmt.Errorf("failed to sign JWT: %w", err)
	}

	return nil
}

// ValidateCredentials validates all credentials fields.
func ValidateCredentials(ctx context.Context, c Credentials) error {
	if c.Name == "" {
		return fmt.Errorf("name is required")
	}
	if c.Email == "" {
		return fmt.Errorf("email is required")
	}

	// Validate GitHub credentials
	if c.GitHubAppID != 0 || c.GitHubPEM != "" {
		if err := ValidateGitHubAppPEM(c.GitHubAppID, c.GitHubPEM); err != nil {
			return fmt.Errorf("GitHub App: %w", err)
		}
	} else if c.PAT != "" {
		if err := ValidateGitHubPAT(ctx, c.PAT); err != nil {
			return fmt.Errorf("GitHub PAT: %w", err)
		}
	}

	// Validate PGP key if present
	if c.PGPPrivateKey != "" {
		// Try OpenPGP first
		if err := ValidatePGPKey(c.PGPPrivateKey); err != nil {
			// Try libsodium
			if err2 := ValidateLibsodiumKey(c.PGPPrivateKey); err2 != nil {
				return fmt.Errorf("PGP key: %w (also tried libsodium: %v)", err, err2)
			}
		}
	}

	return nil
}

// HasPassphrase checks if an OpenPGP key is passphrase-protected.
func HasPassphrase(armoredKey string) bool {
	block, err := armor.Decode(strings.NewReader(armoredKey))
	if err != nil {
		return false
	}

	entityList, err := openpgp.ReadKeyRing(block.Body)
	if err != nil || len(entityList) == 0 {
		return false
	}

	entity := entityList[0]
	return entity.PrivateKey.Encrypted
}
