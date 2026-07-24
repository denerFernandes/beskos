// Package secretbox cifra segredos pequenos em repouso (ex.: o segredo TOTP) com AES-256-GCM.
// A chave vem de uma string de ambiente, derivada por SHA-256. Um valor cifrado carrega o prefixo
// "enc:"; valores sem o prefixo são tratados como texto claro (compat. com dados legados e com
// ambientes sem chave configurada).
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

const prefix = "enc:"

// Box cifra/decifra com uma chave fixa. nil = sem cifra (no-op): ver New.
type Box struct{ key [32]byte }

// New devolve um Box a partir da string de chave. Vazia → nil (cifra desligada; guarda em claro).
func New(secret string) *Box {
	if secret == "" {
		return nil
	}
	return &Box{key: sha256.Sum256([]byte(secret))}
}

// Encrypt devolve "enc:" + base64(nonce||ciphertext). Com Box nil, devolve o texto em claro.
func (b *Box) Encrypt(plain string) (string, error) {
	if b == nil {
		return plain, nil
	}
	gcm, err := b.gcm()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ct := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return prefix + base64.RawURLEncoding.EncodeToString(ct), nil
}

// Decrypt reverte Encrypt. Um valor sem o prefixo "enc:" é devolvido como está (legado/claro).
func (b *Box) Decrypt(stored string) (string, error) {
	if !strings.HasPrefix(stored, prefix) {
		return stored, nil
	}
	if b == nil {
		return "", errors.New("secretbox: valor cifrado sem chave configurada")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(stored, prefix))
	if err != nil {
		return "", err
	}
	gcm, err := b.gcm()
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("secretbox: ciphertext muito curto")
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}

func (b *Box) gcm() (cipher.AEAD, error) {
	block, err := aes.NewCipher(b.key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
