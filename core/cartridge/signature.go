package cartridge

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Signatures close the cartridge supply-chain trust loop. A checksum proves the bytes
// weren't corrupted in transit; a SIGNATURE proves WHO authored them — which matters
// because a cartridge carries executable commands. We use ed25519 from the Go stdlib:
// no external dependency, CGO-free, sovereign. A registry author signs a cartridge's
// bytes with their private key; the operator verifies against locally-trusted public
// keys. Unknown-signer or tampered cartridges are refused.

// Keygen generates an ed25519 keypair, returned base64-encoded (public, private).
func Keygen() (pub string, priv string, err error) {
	pk, sk, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(pk), base64.StdEncoding.EncodeToString(sk), nil
}

// Sign returns a base64 detached signature of data using a base64 ed25519 private key.
func Sign(data []byte, privB64 string) (string, error) {
	sk, err := base64.StdEncoding.DecodeString(privB64)
	if err != nil {
		return "", fmt.Errorf("bad private key: %w", err)
	}
	if len(sk) != ed25519.PrivateKeySize {
		return "", fmt.Errorf("private key must be %d bytes, got %d", ed25519.PrivateKeySize, len(sk))
	}
	return base64.StdEncoding.EncodeToString(ed25519.Sign(sk, data)), nil
}

// VerifySignature checks a base64 signature of data against any of the trusted base64
// public keys. Returns the signer's key (base64) on success, or ok=false. A malformed key
// is skipped rather than fatal (one bad trusted key can't disable the others).
func VerifySignature(data []byte, sigB64 string, trusted []string) (signer string, ok bool) {
	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return "", false
	}
	for _, kb := range trusted {
		pk, err := base64.StdEncoding.DecodeString(kb)
		if err != nil || len(pk) != ed25519.PublicKeySize {
			continue
		}
		if ed25519.Verify(ed25519.PublicKey(pk), data, sig) {
			return kb, true
		}
	}
	return "", false
}

func trustedKeysFile() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".sahayak", "trusted-keys.json"), nil
}

// TrustedKeys returns the locally-trusted publisher public keys (base64).
func TrustedKeys() ([]string, error) {
	path, err := trustedKeysFile()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var keys []string
	if err := json.Unmarshal(b, &keys); err != nil {
		return nil, err
	}
	return keys, nil
}

// AddTrustedKey trusts a publisher public key (base64), idempotently.
func AddTrustedKey(pubB64 string) error {
	if d, err := base64.StdEncoding.DecodeString(pubB64); err != nil || len(d) != ed25519.PublicKeySize {
		return fmt.Errorf("not a valid ed25519 public key (base64, %d bytes)", ed25519.PublicKeySize)
	}
	keys, err := TrustedKeys()
	if err != nil {
		return err
	}
	for _, k := range keys {
		if k == pubB64 {
			return nil
		}
	}
	keys = append(keys, pubB64)
	path, err := trustedKeysFile()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(keys, "", "  ")
	return os.WriteFile(path, b, 0o644)
}
