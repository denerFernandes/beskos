package assistant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// EffectLog dá IDEMPOTÊNCIA às ferramentas de escrita (lei 5.3, §7.5).
//
// Uma tarefa em fundo que reinicia — porque o servidor foi abaixo a meio — reexecuta o ciclo desde
// o princípio. Sem esta guarda, um e-mail já enviado sairia outra vez. Com ela, a segunda tentativa
// reconhece o efeito e devolve o resumo do primeiro, sem tocar em nada.
//
// Só vale para ferramentas de ESCRITA: repetir uma pesquisa é barato e inofensivo, e guardar cada
// leitura só encheria a tabela.
type EffectLog interface {
	// Seen devolve o resumo de um efeito já produzido, se já tiver acontecido.
	Seen(ctx context.Context, fingerprint string) (string, bool, error)
	// Record marca o efeito como feito.
	Record(ctx context.Context, fingerprint, summary string) error
}

// effectFingerprint identifica um efeito por (ferramenta + argumentos).
//
// Os argumentos são NORMALIZADOS antes de serem resumidos: o modelo pode emitir as mesmas chaves
// por outra ordem ou com outros espaços entre execuções, e duas formas do mesmo pedido têm de dar a
// mesma impressão digital — senão a guarda não guardava nada.
//
// Consequência deliberada: dois envios DIFERENTES na mesma tarefa saem os dois, como devem. A
// guarda impede a repetição, não a pluralidade.
func effectFingerprint(tool string, args json.RawMessage) string {
	h := sha256.New()
	h.Write([]byte(tool))
	h.Write([]byte{0})
	h.Write([]byte(canonicalJSON(args)))
	return hex.EncodeToString(h.Sum(nil))
}

// canonicalJSON reescreve o JSON com as chaves por ordem e sem espaços supérfluos. Um valor que não
// desserialize volta tal e qual: é melhor uma impressão digital menos estável do que nenhuma.
func canonicalJSON(raw json.RawMessage) string {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	out, err := json.Marshal(sortKeys(v))
	if err != nil {
		return string(raw)
	}
	return string(out)
}

// sortKeys reconstrói mapas com as chaves ordenadas. O encoding/json já serializa mapas por ordem
// de chave, portanto basta garantir que tudo o que é objeto passa por map[string]any.
func sortKeys(v any) any {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make(map[string]any, len(t))
		for _, k := range keys {
			out[k] = sortKeys(t[k])
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = sortKeys(e)
		}
		return out
	default:
		return v
	}
}
