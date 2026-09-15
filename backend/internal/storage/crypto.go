package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/filecrypt"
)

// Cifra de envelope dos objetos (docs/cifra-ficheiros.md).
//
// Vive AQUI, e não nos handlers, de propósito: são 33 pontos no código a ler e escrever ficheiros —
// correio, chat, wiki, quadros, apresentações, gravações, OnlyOffice, sync — e nenhum deles muda.
// O serviço de storage é o único que fala com o balde, por isso é o único sítio onde a cifra tem de
// existir. Espalhá-la pelos handlers era garantir que um deles ficava esquecido.
//
// SEM CHAVE CONFIGURADA o serviço escreve em claro, como antes. É o que permite pôr isto em
// produção sem um salto: primeiro entra o código, depois entra a chave, e só então os objetos novos
// passam a nascer cifrados. Ler continua a funcionar nos dois casos porque a deteção é pela magia
// no cabeçalho do objeto.

// keyring resolve chaves e mantém em memória as dos tenants, que são poucas e muito relidas.
type keyring struct {
	q   *db.Queries
	kek *filecrypt.KEK

	mu     sync.RWMutex
	tenant map[uuid.UUID][]byte
}

// EnableEncryption liga a cifra. `kekConfig` vem do ambiente (FILE_ENC_KEYS); vazio deixa o serviço
// a escrever em claro. Devolve erro se a configuração existir mas estiver malformada — arrancar com
// uma chave inválida seria pior do que não arrancar.
func (s *Service) EnableEncryption(q *db.Queries, kekConfig string) error {
	kek, err := filecrypt.ParseKEK(kekConfig)
	if err != nil {
		return err
	}
	if kek == nil {
		return nil // sem chave: comportamento antigo, em claro
	}
	s.keys = &keyring{q: q, kek: kek, tenant: map[uuid.UUID][]byte{}}
	return nil
}

// Encrypting diz se os objetos novos nascem cifrados.
func (s *Service) Encrypting() bool { return s.keys != nil }

// tenantFromKey extrai o tenant do prefixo da chave (`<uuid>/…`). As chaves de sistema — as
// gravações antigas, em `recordings/…` — não têm tenant, e a DEK delas é embrulhada pela KEK.
func tenantFromKey(key string) (uuid.UUID, bool) {
	i := strings.IndexByte(key, '/')
	if i <= 0 {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(key[:i])
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

// tenantKey devolve a chave de um tenant, criando-a na primeira utilização.
func (k *keyring) tenantKey(ctx context.Context, tenant uuid.UUID) ([]byte, error) {
	k.mu.RLock()
	cached, ok := k.tenant[tenant]
	k.mu.RUnlock()
	if ok {
		return cached, nil
	}

	row, err := k.q.GetTenantKey(ctx, tenant)
	if err == nil {
		key, uerr := k.kek.Unwrap(row.WrappedKey, int(row.KekVersion))
		if uerr != nil {
			return nil, uerr
		}
		k.store(tenant, key)
		return key, nil
	}

	// Ainda não existe: gera. Duas chamadas em paralelo para o mesmo tenant não podem criar duas
	// chaves — a segunda tornaria ilegível o que a primeira cifrou. O INSERT é ON CONFLICT DO
	// NOTHING; se não devolver linha, alguém ganhou a corrida e relemos a dele.
	fresh, err := filecrypt.NewKey()
	if err != nil {
		return nil, err
	}
	wrapped, version, err := k.kek.Wrap(fresh)
	if err != nil {
		return nil, err
	}
	created, err := k.q.CreateTenantKey(ctx, db.CreateTenantKeyParams{
		TenantID: tenant, WrappedKey: wrapped, KekVersion: int32(version),
	})
	if err != nil {
		// Sem linha devolvida: houve corrida. Relê a chave que o outro criou.
		row, rerr := k.q.GetTenantKey(ctx, tenant)
		if rerr != nil {
			return nil, fmt.Errorf("tenant key %s: %w", tenant, err)
		}
		key, uerr := k.kek.Unwrap(row.WrappedKey, int(row.KekVersion))
		if uerr != nil {
			return nil, uerr
		}
		k.store(tenant, key)
		return key, nil
	}
	key, err := k.kek.Unwrap(created.WrappedKey, int(created.KekVersion))
	if err != nil {
		return nil, err
	}
	k.store(tenant, key)
	return key, nil
}

func (k *keyring) store(tenant uuid.UUID, key []byte) {
	k.mu.Lock()
	k.tenant[tenant] = key
	k.mu.Unlock()
}

// wrapForKey cria a chave de um objeto novo e devolve-a em claro e embrulhada. Nada disto vai à
// base de dados: a versão embrulhada viaja no cabeçalho do objeto, e é o que torna a escrita uma
// operação só. Ver o comentário do pacote filecrypt.
func (k *keyring) wrapForKey(ctx context.Context, blobKey string) (dek []byte, h filecrypt.Header, err error) {
	dek, err = filecrypt.NewKey()
	if err != nil {
		return nil, h, err
	}
	h.ChunkSize = filecrypt.DefaultChunkSize

	if tenant, ok := tenantFromKey(blobKey); ok {
		tk, err := k.tenantKey(ctx, tenant)
		if err != nil {
			return nil, h, err
		}
		wrapped, err := filecrypt.WrapDEK(tk, dek)
		if err != nil {
			return nil, h, err
		}
		h.TenantWrapped, h.WrappedKey = true, wrapped
		return dek, h, nil
	}

	// Objeto de sistema (as gravações antigas, em `recordings/…`): embrulha-se com a KEK, e fica
	// de fora da eliminação por tenant. As gravações novas levam prefixo de tenant por isso mesmo.
	wrapped, version, err := k.kek.Wrap(dek)
	if err != nil {
		return nil, h, err
	}
	h.KEKVersion, h.WrappedKey = version, wrapped
	return dek, h, nil
}

// unwrapFromHeader abre a chave que veio dentro do objeto.
func (k *keyring) unwrapFromHeader(ctx context.Context, blobKey string, h filecrypt.Header) ([]byte, error) {
	if !h.TenantWrapped {
		return k.kek.Unwrap(h.WrappedKey, h.KEKVersion)
	}
	tenant, ok := tenantFromKey(blobKey)
	if !ok {
		return nil, fmt.Errorf("storage: %q claims to be tenant-wrapped but the key has no prefix", blobKey)
	}
	tk, err := k.tenantKey(ctx, tenant)
	if err != nil {
		return nil, err
	}
	return filecrypt.UnwrapDEK(tk, h.WrappedKey)
}

// abre prepara a leitura de um objeto: lê o cabeçalho, decide se está cifrado e devolve o leitor
// certo. Um objeto sem a nossa magia é devolvido tal como está — é assim que o conteúdo anterior à
// cifra continua a servir-se enquanto a migração não lhe chega.
func (s *Service) decrypt(ctx context.Context, key string, obj io.ReadCloser) (io.ReadCloser, error) {
	h, read, err := readHeader(obj)
	if errors.Is(err, filecrypt.ErrNotEncrypted) {
		// Anterior à cifra: devolve o que foi lido colado ao resto.
		return join(io.MultiReader(bytes.NewReader(read), obj), obj), nil
	}
	if err != nil {
		obj.Close()
		return nil, err
	}
	if s.keys == nil {
		obj.Close()
		return nil, errors.New("storage: object is encrypted but encryption is off (FILE_ENC_KEYS missing)")
	}
	dek, err := s.keys.unwrapFromHeader(ctx, key, h)
	if err != nil {
		obj.Close()
		return nil, err
	}
	r, err := filecrypt.DecryptReader(obj, h, dek)
	if err != nil {
		obj.Close()
		return nil, err
	}
	return join(r, obj), nil
}

// lerCabecalho consome o cabeçalho fixo e a chave embrulhada. Devolve também os bytes lidos, para
// quem precise de os recolar quando afinal o objeto não estava cifrado.
func readHeader(r io.Reader) (filecrypt.Header, []byte, error) {
	fixed := make([]byte, filecrypt.FixedHeaderSize)
	n, err := io.ReadFull(r, fixed)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return filecrypt.Header{}, fixed[:n], err
	}
	h, perr := filecrypt.ParseHeader(fixed[:n])
	if perr != nil {
		return filecrypt.Header{}, fixed[:n], perr
	}
	if l := h.WrappedKeyLen(); l > 0 {
		if _, err := io.ReadFull(r, h.WrappedKey); err != nil {
			return filecrypt.Header{}, fixed[:n], filecrypt.ErrCorrupt
		}
	}
	return h, fixed[:n], nil
}

// juntar cola um leitor a um Closer — o objeto do armazenamento tem de ser fechado na mesma.
func join(r io.Reader, c io.Closer) io.ReadCloser { return &readCloser{r: r, c: c} }

type readCloser struct {
	r io.Reader
	c io.Closer
}

func (rc *readCloser) Read(p []byte) (int, error) { return rc.r.Read(p) }
func (rc *readCloser) Close() error               { return rc.c.Close() }
