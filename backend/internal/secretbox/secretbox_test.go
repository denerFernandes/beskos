package secretbox

import "testing"

func TestRoundTrip(t *testing.T) {
	b := New("uma-chave-de-teste-bem-comprida")
	plain := "alice@acme.com:s3nha-secreta"
	enc, err := b.Encrypt(plain)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if enc == plain || len(enc) < 4 || enc[:4] != "enc:" {
		t.Fatalf("esperava ciphertext com prefixo enc:, veio %q", enc)
	}
	got, err := b.Decrypt(enc)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if got != plain {
		t.Fatalf("roundtrip: got %q want %q", got, plain)
	}
}

func TestNonceIsRandom(t *testing.T) {
	b := New("chave")
	a, _ := b.Encrypt("x")
	c, _ := b.Encrypt("x")
	if a == c {
		t.Fatal("dois ciphertexts do mesmo texto deviam diferir (nonce aleatório)")
	}
}

// TestTamperFails flips a bit near the middle of the base64 payload, not the last character:
// the last base64 character of a padded group can encode bits that never surface in the decoded
// bytes, so flipping it sometimes leaves the actual ciphertext unchanged and this test flaky
// (found running it with -count=5: 2 failures in 5 runs, unrelated to any real bug).
func TestTamperFails(t *testing.T) {
	b := New("chave")
	enc, _ := b.Encrypt("segredo")
	mid := 4 + (len(enc)-4)/2 // skip the "enc:" prefix, tamper a byte guaranteed to matter
	tampered := enc[:mid] + string(enc[mid]^1) + enc[mid+1:]
	if _, err := b.Decrypt(tampered); err == nil {
		t.Fatal("decrypt de ciphertext adulterado deveria falhar (GCM autentica)")
	}
}

func TestWrongKeyFails(t *testing.T) {
	enc, _ := New("chave-a").Encrypt("segredo")
	if _, err := New("chave-b").Decrypt(enc); err == nil {
		t.Fatal("decrypt com chave errada deveria falhar")
	}
}

func TestPlaintextPassthrough(t *testing.T) {
	// Valor sem prefixo enc: (legado/dev sem chave) volta como está.
	b := New("chave")
	if v, err := b.Decrypt("texto-claro"); err != nil || v != "texto-claro" {
		t.Fatalf("passthrough: got %q err %v", v, err)
	}
}

func TestNilBox(t *testing.T) {
	var b *Box // sem chave (dev): no-op
	if v, _ := b.Encrypt("x"); v != "x" {
		t.Fatalf("box nil deveria devolver o texto em claro, veio %q", v)
	}
	if _, err := b.Decrypt("enc:abc"); err == nil {
		t.Fatal("box nil não pode decifrar um valor cifrado")
	}
}
