package cartridge

import "testing"

func TestSignVerifyRoundTrip(t *testing.T) {
	pub, priv, err := Keygen()
	if err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"name":"redis"}`)
	sig, err := Sign(data, priv)
	if err != nil {
		t.Fatal(err)
	}
	signer, ok := VerifySignature(data, sig, []string{pub})
	if !ok || signer != pub {
		t.Fatalf("valid signature should verify against its public key (ok=%v)", ok)
	}
}

func TestVerifyRejectsTamperedData(t *testing.T) {
	pub, priv, _ := Keygen()
	sig, _ := Sign([]byte(`{"name":"redis"}`), priv)
	if _, ok := VerifySignature([]byte(`{"name":"redis-EVIL"}`), sig, []string{pub}); ok {
		t.Fatal("a tampered cartridge must NOT verify")
	}
}

func TestVerifyRejectsUntrustedSigner(t *testing.T) {
	_, priv, _ := Keygen()     // signed by one key…
	otherPub, _, _ := Keygen() // …but only a DIFFERENT key is trusted
	sig, _ := Sign([]byte("x"), priv)
	if _, ok := VerifySignature([]byte("x"), sig, []string{otherPub}); ok {
		t.Fatal("a signature from an untrusted key must NOT verify")
	}
}

func TestAddTrustedKeyValidates(t *testing.T) {
	if err := AddTrustedKey("not-a-key"); err == nil {
		t.Error("AddTrustedKey should reject a non-ed25519 key")
	}
}
