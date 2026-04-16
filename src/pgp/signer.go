// Package pgp provides OpenPGP signing for APG packages using ProtonMail/go-crypto.
package pgp

import (
	"bytes"
	"fmt"
	"os"

	pgpcrypto "github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

// Sign creates a detached armored signature for pkgPath.
// Writes <pkgPath>.sig alongside the package file.
// passphrase may be empty if the key has no passphrase.
func Sign(pkgPath, armoredPrivKey, passphrase string) error {
	entity, err := readKey(armoredPrivKey, passphrase)
	if err != nil {
		return fmt.Errorf("load pgp key: %w", err)
	}

	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return fmt.Errorf("read package: %w", err)
	}

	var sigBuf bytes.Buffer
	w, err := armor.Encode(&sigBuf, "PGP SIGNATURE", nil)
	if err != nil {
		return fmt.Errorf("armor encode: %w", err)
	}
	if err := pgpcrypto.DetachSign(w, entity, bytes.NewReader(data), nil); err != nil {
		return fmt.Errorf("detach sign: %w", err)
	}
	w.Close()

	return os.WriteFile(pkgPath+".sig", sigBuf.Bytes(), 0644)
}

// GenerateRevocationCert produces an armored revocation certificate for the key.
func GenerateRevocationCert(armoredPrivKey, passphrase string) ([]byte, error) {
	entity, err := readKey(armoredPrivKey, passphrase)
	if err != nil {
		return nil, fmt.Errorf("load pgp key: %w", err)
	}

	var buf bytes.Buffer
	w, err := armor.Encode(&buf, "PGP PUBLIC KEY BLOCK", nil)
	if err != nil {
		return nil, err
	}

	// Generate revocation signature
	cfg := &packet.Config{}
	if err := entity.RevokeKey(0, "Key compromised — revoked by owner", cfg); err != nil {
		return nil, fmt.Errorf("revoke key: %w", err)
	}
	if err := entity.Serialize(w); err != nil {
		return nil, fmt.Errorf("serialize revoked key: %w", err)
	}
	w.Close()
	return buf.Bytes(), nil
}

// ExportPublicKey returns the armored public key for the given private key.
func ExportPublicKey(armoredPrivKey, passphrase string) ([]byte, error) {
	entity, err := readKey(armoredPrivKey, passphrase)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	w, err := armor.Encode(&buf, "PGP PUBLIC KEY BLOCK", nil)
	if err != nil {
		return nil, err
	}
	if err := entity.Serialize(w); err != nil {
		return nil, err
	}
	w.Close()
	return buf.Bytes(), nil
}

// HasPassphrase attempts to decrypt the key with an empty passphrase.
// Returns true if the key IS protected (empty passphrase fails).
func HasPassphrase(armoredPrivKey string) bool {
	_, err := readKey(armoredPrivKey, "")
	return err != nil
}

// readKey decodes an armored private key and decrypts it with passphrase.
func readKey(armoredPrivKey, passphrase string) (*pgpcrypto.Entity, error) {
	block, err := armor.Decode(bytes.NewReader([]byte(armoredPrivKey)))
	if err != nil {
		return nil, fmt.Errorf("decode armor: %w", err)
	}

	entities, err := pgpcrypto.ReadKeyRing(block.Body)
	if err != nil {
		return nil, fmt.Errorf("read key ring: %w", err)
	}
	if len(entities) == 0 {
		return nil, fmt.Errorf("no keys found in armor")
	}

	entity := entities[0]

	// Decrypt private key if passphrase provided
	pp := []byte(passphrase)
	if entity.PrivateKey != nil && entity.PrivateKey.Encrypted {
		if err := entity.PrivateKey.Decrypt(pp); err != nil {
			return nil, fmt.Errorf("decrypt private key: wrong passphrase")
		}
	}
	for _, sub := range entity.Subkeys {
		if sub.PrivateKey != nil && sub.PrivateKey.Encrypted {
			_ = sub.PrivateKey.Decrypt(pp) // best-effort for subkeys
		}
	}

	return entity, nil
}
