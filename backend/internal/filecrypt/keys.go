package filecrypt

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// Hierarquia de chaves:  KEK (ambiente) → chave do tenant → DEK (uma por blob) → objeto.
//
// A KEK nunca cifra um objeto: cifra chaves. É isso que torna a rotação barata — trocar a KEK
// re-embrulha uma tabela pequena e não reescreve um único byte do armazenamento.

// KEK é o conjunto de chaves-mestras conhecidas, por versão. Guarda-se mais do que uma para que
// uma rotação possa decorrer em fundo: o material antigo continua a abrir-se enquanto não for
// re-embrulhado.
type KEK struct {
	byVersion map[int][KeySize]byte
	current   int
}

// ParseKEK lê a configuração do ambiente. Formato: "1:<hex de 64>,2:<hex de 64>". A versão mais
// alta é a corrente — a que embrulha material novo.
//
// Aceita também uma chave sem versão (só o hex), tratada como versão 1, para não obrigar a mexer no
// formato quando só existe uma.
func ParseKEK(s string) (*KEK, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil // cifra desligada: ver Service.
	}
	k := &KEK{byVersion: map[int][KeySize]byte{}}
	for _, parte := range strings.Split(s, ",") {
		parte = strings.TrimSpace(parte)
		if parte == "" {
			continue
		}
		versao, hexa := 1, parte
		if i := strings.Index(parte, ":"); i > 0 {
			n, err := strconv.Atoi(parte[:i])
			if err != nil {
				return nil, fmt.Errorf("filecrypt: versão de KEK inválida em %q", parte)
			}
			versao, hexa = n, parte[i+1:]
		}
		bruto, err := hex.DecodeString(strings.TrimSpace(hexa))
		if err != nil {
			return nil, fmt.Errorf("filecrypt: KEK v%d não é hexadecimal: %w", versao, err)
		}
		if len(bruto) != KeySize {
			return nil, fmt.Errorf("filecrypt: KEK v%d tem %d bytes, precisa de %d", versao, len(bruto), KeySize)
		}
		var chave [KeySize]byte
		copy(chave[:], bruto)
		k.byVersion[versao] = chave
		if versao > k.current {
			k.current = versao
		}
	}
	if len(k.byVersion) == 0 {
		return nil, fmt.Errorf("filecrypt: FILE_ENC_KEYS definida mas sem nenhuma chave válida")
	}
	return k, nil
}

// CurrentVersion é a versão que embrulha material novo.
func (k *KEK) CurrentVersion() int { return k.current }

// NewKey gera material de chave aleatório (chave de tenant ou DEK).
func NewKey() ([]byte, error) {
	b := make([]byte, KeySize)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

// Wrap embrulha `key` com a KEK da versão corrente.
func (k *KEK) Wrap(key []byte) ([]byte, int, error) {
	mestra, ok := k.byVersion[k.current]
	if !ok {
		return nil, 0, fmt.Errorf("filecrypt: KEK v%d desconhecida", k.current)
	}
	out, err := wrapWith(mestra[:], key)
	return out, k.current, err
}

// WrapWithVersion embrulha com uma versão específica. Serve à rotação, que precisa de re-embrulhar
// para a versão nova sabendo qual era a antiga.
func (k *KEK) WrapWithVersion(key []byte, versao int) ([]byte, error) {
	mestra, ok := k.byVersion[versao]
	if !ok {
		return nil, fmt.Errorf("filecrypt: KEK v%d desconhecida", versao)
	}
	return wrapWith(mestra[:], key)
}

// Unwrap abre material embrulhado por uma dada versão da KEK.
func (k *KEK) Unwrap(wrapped []byte, versao int) ([]byte, error) {
	mestra, ok := k.byVersion[versao]
	if !ok {
		// Acontece se a KEK antiga for retirada do ambiente antes de a rotação acabar. É um erro
		// de operação, não de dados — e é melhor falhar alto do que servir lixo.
		return nil, fmt.Errorf("filecrypt: KEK v%d não está configurada; material antigo fica ilegível sem ela", versao)
	}
	return unwrapWith(mestra[:], wrapped)
}

// WrapDEK embrulha uma DEK com a chave do tenant (não com a KEK): é o degrau do meio que permite
// tornar os dados de um tenant ilegíveis apagando uma única linha.
func WrapDEK(tenantKey, dek []byte) ([]byte, error) { return wrapWith(tenantKey, dek) }

// UnwrapDEK abre uma DEK com a chave do tenant.
func UnwrapDEK(tenantKey, wrapped []byte) ([]byte, error) { return unwrapWith(tenantKey, wrapped) }

// wrapWith cifra material de chave com AES-256-GCM: nonce ‖ cifrado ‖ selo.
func wrapWith(chave, material []byte) ([]byte, error) {
	gcm, err := newGCM(normaliza(chave))
	if err != nil {
		return nil, err
	}
	n := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(n); err != nil {
		return nil, err
	}
	return gcm.Seal(n, n, material, nil), nil
}

func unwrapWith(chave, wrapped []byte) ([]byte, error) {
	gcm, err := newGCM(normaliza(chave))
	if err != nil {
		return nil, err
	}
	ns := gcm.NonceSize()
	if len(wrapped) < ns {
		return nil, ErrCorrupt
	}
	out, err := gcm.Open(nil, wrapped[:ns], wrapped[ns:], nil)
	if err != nil {
		return nil, ErrCorrupt
	}
	return out, nil
}

// normaliza aceita chaves que não tenham exatamente 32 bytes, derivando por SHA-256 — o mesmo que
// o secretbox faz para o TOTP. Chaves geradas por nós já vêm com 32 e passam intactas.
func normaliza(chave []byte) []byte {
	if len(chave) == KeySize {
		return chave
	}
	soma := sha256.Sum256(chave)
	return soma[:]
}
