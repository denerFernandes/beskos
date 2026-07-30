package stalwart

import (
	"context"
	"encoding/json"
	"fmt"
)

// Script de sistema na fase DATA — usado para acrescentar o cabeçalho que identifica o cliente ao
// relay de saída (X-SES-TENANT).
//
// PORQUÊ AQUI E NÃO NO NOSSO CÓDIGO: o correio dos clientes vai do cliente para o Stalwart e do
// Stalwart para o relay. Não passa pela nossa API em momento nenhum, por isso o cabeçalho tem de
// ser posto por quem está no caminho — e o Stalwart deixa fazê-lo com um Sieve na fase DATA.
//
// PORQUE É SEGURO: se o script não corresponder a nada, não acrescenta cabeçalho e a mensagem segue
// na mesma. Perde-se o isolamento daquela mensagem, não a mensagem. Um relé nosso no meio, se
// falhasse, pararia o correio — foi por isso que se preferiu esta via.

// ScriptTenantSES devolve o Sieve que acrescenta o cabeçalho do tenant.
//
// O nome do tenant é o domínio com hífens no lugar dos pontos (ver ses.TenantName), porque o SES não
// aceita pontos. O Sieve não tem substituição de texto, mas tem CAPTURAS: cada `*` do padrão guarda
// um pedaço, e voltam a juntar-se com hífens. Os padrões são tentados do mais específico para o
// menos, porque o `*` é ganancioso — com `a.b.c`, o padrão de dois pedaços daria `a.b` e `c`.
//
// Vai até cinco níveis. Um domínio com mais do que isso ficaria com o nome errado, e é por isso que
// o último ramo não existe em vez de adivinhar: sem correspondência, não há cabeçalho, e a mensagem
// sai à mesma — a falha é visível nas métricas em vez de silenciosa no nome.
func ScriptTenantSES() string {
	return `require ["editheader", "envelope", "variables"];

# Identifica o cliente ao relay de saída. O nome é o domínio do remetente com hífens no lugar dos
# pontos — ver internal/ses.TenantName, que tem de continuar a produzir exatamente o mesmo.
if envelope :domain :matches "from" "*.*.*.*.*" {
    addheader :last "X-SES-TENANT" "${1}-${2}-${3}-${4}-${5}";
} elsif envelope :domain :matches "from" "*.*.*.*" {
    addheader :last "X-SES-TENANT" "${1}-${2}-${3}-${4}";
} elsif envelope :domain :matches "from" "*.*.*" {
    addheader :last "X-SES-TENANT" "${1}-${2}-${3}";
} elsif envelope :domain :matches "from" "*.*" {
    addheader :last "X-SES-TENANT" "${1}-${2}";
}
`
}

// UpsertSieveScript cria ou atualiza um script de sistema e devolve o id.
func (c *Client) UpsertSieveScript(ctx context.Context, nome, conteudo string) (string, error) {
	// Procura primeiro: o `set` do registo não tem "cria ou atualiza", e provisionar duas vezes não
	// pode deixar dois scripts com o mesmo nome.
	id, err := c.sieveScriptID(ctx, nome)
	if err != nil {
		return "", err
	}

	campos := map[string]any{"name": nome, "contents": conteudo, "isActive": true}
	args := map[string]any{}
	if id != "" {
		args["update"] = map[string]any{id: campos}
	} else {
		args["create"] = map[string]any{"s1": campos}
	}

	raw, err := c.call(ctx, "x:SieveSystemScript/set", args)
	if err != nil {
		return "", err
	}
	var r setResult
	if err := json.Unmarshal(raw, &r); err != nil {
		return "", fmt.Errorf("decode SieveSystemScript/set: %w", err)
	}
	if len(r.NotCreated) > 0 || len(r.NotUpdated) > 0 {
		return "", fmt.Errorf("stalwart recusou o script: criados=%s atualizados=%s",
			mustJSON(r.NotCreated), mustJSON(r.NotUpdated))
	}
	if id != "" {
		return id, nil
	}
	return firstCreatedID(&r)
}

// sieveScriptID procura um script de sistema pelo nome. Devolve "" se não existir.
func (c *Client) sieveScriptID(ctx context.Context, nome string) (string, error) {
	raw, err := c.call(ctx, "x:SieveSystemScript/query", map[string]any{})
	if err != nil {
		return "", err
	}
	var q struct {
		IDs []string `json:"ids"`
	}
	if err := json.Unmarshal(raw, &q); err != nil {
		return "", fmt.Errorf("decode SieveSystemScript/query: %w", err)
	}
	if len(q.IDs) == 0 {
		return "", nil
	}
	raw, err = c.call(ctx, "x:SieveSystemScript/get", map[string]any{"ids": q.IDs})
	if err != nil {
		return "", err
	}
	var g struct {
		List []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"list"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		return "", fmt.Errorf("decode SieveSystemScript/get: %w", err)
	}
	for _, s := range g.List {
		if s.Name == nome {
			return s.ID, nil
		}
	}
	return "", nil
}

// GetDataStageScript devolve a expressão que hoje escolhe o script da fase DATA, tal e qual — para
// se poder repor se algo correr mal.
func (c *Client) GetDataStageScript(ctx context.Context) (json.RawMessage, error) {
	raw, err := c.call(ctx, "x:MtaStageData/get", map[string]any{"ids": []string{singletonID}})
	if err != nil {
		return nil, err
	}
	var r struct {
		List []json.RawMessage `json:"list"`
	}
	if err := json.Unmarshal(raw, &r); err != nil || len(r.List) == 0 {
		return nil, fmt.Errorf("MtaStageData/get não devolveu nada: %v", err)
	}
	return r.List[0], nil
}

// SetDataStageScript faz a fase DATA correr o script indicado.
func (c *Client) SetDataStageScript(ctx context.Context, nomeDoScript string) error {
	// CAMINHOS DE PATCH, não um objeto aninhado. O JMAP do Stalwart atualiza por ponteiro
	// (`script/match/0/if`); mandar `script` como objeto com um `match` lá dentro é recusado com
	// `invalidPatch` a apontar precisamente `script/match`.
	//
	// SÓ NAS PORTAS DE SUBMISSÃO. A fase DATA corre em TODAS as sessões, incluindo o correio que
	// ENTRA na porta 25 — e aí `env_from.domain` é o domínio de quem nos escreveu. Sem esta
	// condição, uma mensagem do Gmail chegaria com `X-SES-TENANT: gmail-com` colado: lixo no
	// correio dos clientes e um número errado nas métricas do relay.
	raw, err := c.call(ctx, "x:MtaStageData/set", map[string]any{
		"update": map[string]any{
			singletonID: map[string]any{
				"script/else":         "''",
				"script/match/0/if":   "local_port == 465 || local_port == 587",
				"script/match/0/then": fmt.Sprintf("'%s'", nomeDoScript),
			},
		},
	})
	if err != nil {
		return err
	}
	var r setResult
	if err := json.Unmarshal(raw, &r); err != nil {
		return fmt.Errorf("decode MtaStageData/set: %w", err)
	}
	if len(r.NotUpdated) > 0 {
		return fmt.Errorf("stalwart recusou a fase DATA: %s", mustJSON(r.NotUpdated))
	}
	return nil
}
