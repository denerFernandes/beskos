package stalwart

import (
	"context"
	"encoding/json"
	"fmt"
)

// Armazenamento de blocos do Stalwart — onde ele guarda a mensagem crua e os anexos.
//
// É um objeto SINGLETON no registo dele (`x:BlobStore`, ver resources/schema/schema.json do
// Stalwart). Mexer aqui muda onde o correio é escrito, por isso as duas operações estão separadas:
// ler devolve o que lá está tal e qual, para se poder guardar antes de escrever por cima.

// BlobStore é a configuração como o Stalwart a devolve. Fica em JSON cru de propósito: as variantes
// são muitas (Default, S3, Azure, FileSystem, PostgreSql…) e este código só precisa de as
// transportar, não de as compreender. Modelar todas seria inventar trabalho para o dia em que o
// Stalwart acrescentar mais uma.
type BlobStore struct {
	ID     string
	Config json.RawMessage
}

// singletonID é o id dos objetos únicos do registo do Stalwart. Não é convenção nossa: no código
// dele é o número 20080258862541, que na base32 de alfabeto próprio que ele usa para ids se escreve
// exatamente assim. Um `get` com `ids: null` devolve lista vazia e o `query` nem existe para estes
// objetos, por isso o id tem de ir explícito.
const singletonID = "singleton"

// GetBlobStoreRaw devolve a resposta do Stalwart tal e qual, para diagnóstico.
func (c *Client) GetBlobStoreRaw(ctx context.Context) (json.RawMessage, error) {
	return c.call(ctx, "x:BlobStore/get", map[string]any{"ids": []string{singletonID}})
}

// GetBlobStore lê a configuração atual.
func (c *Client) GetBlobStore(ctx context.Context) (*BlobStore, error) {
	raw, err := c.GetBlobStoreRaw(ctx)
	if err != nil {
		return nil, err
	}
	var r struct {
		List []json.RawMessage `json:"list"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("decode BlobStore/get: %w", err)
	}
	if len(r.List) == 0 {
		return nil, fmt.Errorf("stalwart: BlobStore/get não devolveu lista; resposta crua: %s", raw)
	}
	return &BlobStore{ID: singletonID, Config: r.List[0]}, nil
}

// S3BlobStore é a variante S3, com os nomes de campo do esquema do Stalwart.
type S3BlobStore struct {
	Bucket    string
	Endpoint  string
	Region    string
	AccessKey string
	SecretKey string
	KeyPrefix string
}

// SetBlobStoreS3 aponta o armazenamento de blocos para um destino S3.
//
// O `verifyAfterWrite` fica LIGADO: o Stalwart faz um HEAD depois de cada escrita para se defender
// de armazenamentos que confirmam antes de tornar o objeto visível. Desligá-lo pouparia uma ida à
// rede por mensagem e trocaria isso por perder correio em silêncio.
func (c *Client) SetBlobStoreS3(ctx context.Context, id string, s S3BlobStore) error {
	cfg := map[string]any{
		"@type":  "S3",
		"bucket": s.Bucket,
		"region": map[string]any{
			"@type":          "Custom",
			"customEndpoint": s.Endpoint,
			"customRegion":   s.Region,
		},
		"accessKey": s.AccessKey,
		"secretKey": map[string]any{"@type": "Value", "secret": s.SecretKey},
		"keyPrefix": s.KeyPrefix,

		"maxRetries":        3,
		"timeout":           30000,
		"verifyAfterWrite":  true,
		"allowInvalidCerts": false,
	}
	raw, err := c.call(ctx, "x:BlobStore/set", map[string]any{
		"update": map[string]any{id: cfg},
	})
	if err != nil {
		return err
	}
	var r setResult
	if err := json.Unmarshal(raw, &r); err != nil {
		return fmt.Errorf("decode BlobStore/set: %w", err)
	}
	if len(r.NotUpdated) > 0 {
		return fmt.Errorf("stalwart recusou a alteração: %s", string(mustJSON(r.NotUpdated)))
	}
	return nil
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// ReloadSettings faz o Stalwart reler a configuração do registo.
//
// É NECESSÁRIO depois de mudar o armazenamento de blocos: o `set` grava a definição, mas o processo
// continua a usar a que tem em memória — e o correio novo continua a ir para o sítio antigo, sem
// erro nenhum, que é a forma mais silenciosa possível de uma alteração não acontecer.
//
// No modelo dele isto é um objeto de ação criado no registo, não um método próprio: `x:Action/set`
// com a variante `ReloadSettings`. A alteração é difundida pelo cluster.
func (c *Client) ReloadSettings(ctx context.Context) error {
	raw, err := c.call(ctx, "x:Action/set", map[string]any{
		"create": map[string]any{"a1": map[string]any{"@type": "ReloadSettings"}},
	})
	if err != nil {
		return err
	}
	var r setResult
	if err := json.Unmarshal(raw, &r); err != nil {
		return fmt.Errorf("decode Action/set: %w", err)
	}
	if len(r.NotCreated) > 0 {
		return fmt.Errorf("recarga recusada: %s", string(mustJSON(r.NotCreated)))
	}
	return nil
}
