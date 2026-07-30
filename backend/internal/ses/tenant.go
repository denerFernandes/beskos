package ses

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Tenants do SES — isolamento de reputação entre clientes.
//
// O PROBLEMA QUE ISTO RESOLVE. Todas as identidades vivem na mesma conta AWS, e a reputação é da
// CONTA: devoluções e queixas de um cliente degradam-na para todos, e a AWS estrangula ou suspende
// a conta inteira. Um cliente com uma lista comprada pode parar o correio de toda a gente.
//
// Um tenant é um contentor lógico com métricas e estado de envio PRÓPRIOS. Quando a reputação de um
// tenant se degrada, a AWS pausa esse tenant — não a conta. É a diferença entre perder um cliente e
// perder o serviço.
//
// COMO SE LIGA. Criar o tenant e associar-lhe a identidade do domínio não chega: o envio tem de
// dizer a que tenant pertence. Pela API é um parâmetro; por SMTP — que é o nosso caso, porque o
// Stalwart entrega ao SES por SMTP — é o cabeçalho `X-SES-TENANT`. Ver docs/manager.md: quem o
// acrescenta é um Sieve do sistema na fase DATA, e não este pacote.

// TenantName converte um domínio no nome do tenant SES correspondente.
//
// O SES só aceita letras, números, hífen e underscore (até 64) — o ponto do domínio está fora. A
// transformação é determinística nos dois sentidos, de propósito: quem olhar para o painel da AWS
// tem de conseguir dizer de que cliente é aquele tenant sem consultar a nossa base de dados.
func TenantName(domain string) string {
	n := strings.ToLower(strings.TrimSpace(domain))
	n = strings.NewReplacer(".", "-", "@", "-", "/", "-").Replace(n)
	if len(n) > 64 {
		n = n[:64]
	}
	return n
}

// CreateTenant cria o tenant. Idempotente: já existir é sucesso, porque o provisionamento repete.
func (c *Client) CreateTenant(ctx context.Context, name string) error {
	body, _ := json.Marshal(map[string]any{
		"TenantName": name,
		// Lista de supressão PRÓPRIA. Sem isto, um endereço que devolve para um cliente fica
		// suprimido para todos os outros — a supressão da conta é partilhada, e um cliente não deve
		// poder impedir outro de escrever a um destinatário legítimo.
		"SuppressionAttributes": map[string]any{
			"SuppressionScope":  "TENANT",
			"SuppressedReasons": []string{"BOUNCE", "COMPLAINT"},
		},
	})
	status, out, err := c.do(ctx, http.MethodPost, "/v2/email/tenants", body)
	if err != nil {
		return err
	}
	if status == http.StatusOK || status == http.StatusCreated {
		return nil
	}
	if jaExiste(status, out) {
		return nil
	}
	return fmt.Errorf("ses create tenant %q: HTTP %d: %s", name, status, out)
}

// AssociateIdentity liga a identidade do domínio ao tenant. É isto que faz as métricas e o estado
// de envio daquele domínio serem contabilizados à parte.
//
// A API pede o ARN do recurso, que se monta a partir da região e da conta. O identificador da conta
// vem de SES_ACCOUNT_ID: pedi-lo ao STS seria uma chamada e uma permissão a mais para obter um
// número que não muda.
func (c *Client) AssociateIdentity(ctx context.Context, tenant, domain string) error {
	if c.accountID == "" {
		return fmt.Errorf("ses: SES_ACCOUNT_ID em falta — sem ele não se monta o ARN da identidade")
	}
	arn := fmt.Sprintf("arn:aws:ses:%s:%s:identity/%s", c.region, c.accountID, domain)
	body, _ := json.Marshal(map[string]any{
		"TenantName":  tenant,
		"ResourceArn": arn,
	})
	status, out, err := c.do(ctx, http.MethodPost, "/v2/email/tenants/resources", body)
	if err != nil {
		return err
	}
	if status == http.StatusOK || status == http.StatusCreated || jaExiste(status, out) {
		return nil
	}
	return fmt.Errorf("ses associate %q ao tenant %q: HTTP %d: %s", domain, tenant, status, out)
}

// TenantSendingStatus devolve o estado de envio do tenant: ENABLED, REINSTATED ou DISABLED.
//
// DISABLED é o sinal que interessa vigiar — quer dizer que a AWS pausou aquele cliente por
// reputação. Descobrir isso pelo painel é descobrir tarde.
func (c *Client) TenantSendingStatus(ctx context.Context, name string) (string, error) {
	// POST com o nome no CORPO, e não um GET com o nome no caminho: o SES v2 usa esta forma para o
	// GetTenant. Adivinhei a outra e a consulta devolvia sempre "não existe" logo a seguir a criar.
	body, _ := json.Marshal(map[string]any{"TenantName": name})
	status, out, err := c.do(ctx, http.MethodPost, "/v2/email/tenants/get", body)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("ses get tenant %q: HTTP %d: %s", name, status, out)
	}
	var r struct {
		Tenant struct {
			SendingStatus string `json:"SendingStatus"`
		} `json:"Tenant"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return "", fmt.Errorf("decode tenant: %w", err)
	}
	return r.Tenant.SendingStatus, nil
}

// jaExiste reconhece a resposta de "o recurso já lá está". O provisionamento repete-se sempre que
// um passo anterior falha, por isso criar duas vezes tem de ser inofensivo.
//
// Duas formas, porque o SES usa as duas: o nome da exceção (`AlreadyExistsException`) e uma frase
// em inglês corrente ("Tenant with name X already exists in account Y"). Procurar só a primeira
// fazia o comando de retroação falhar em tudo o que já estivesse criado.
func jaExiste(status int, corpo []byte) bool {
	if status == http.StatusConflict {
		return true
	}
	if status != http.StatusBadRequest {
		return false
	}
	// Qualquer 400 que diga "already" é uma repetição, não um erro: a SES usa pelo menos três
	// formas — o nome da exceção, "already exists" e "has already been associated". Perseguir cada
	// frase é uma corrida que se perde; o que importa é a categoria.
	return strings.Contains(strings.ToLower(string(corpo)), "already")
}

// --- Conjuntos de configuração ---------------------------------------------------------------
//
// PORQUE EXISTEM AQUI, ao lado dos tenants: resolvem o mesmo problema por um caminho que NÃO exige
// tocar no correio. Um tenant só recebe tráfego se cada mensagem trouxer o cabeçalho `X-SES-TENANT`
// — e as mensagens dos clientes não passam pelo nosso código, passam pelo Stalwart. Um conjunto de
// configuração, ao contrário, pode ser colado à IDENTIDADE do domínio, e a AWS aplica-o
// «every time email is sent from the identity». Nada no caminho do correio muda.
//
// O que se ganha sem cabeçalho nenhum: métricas de reputação por cliente (saber de QUEM são as
// devoluções) e a capacidade de suspender o envio de UM cliente sem tocar na conta.
//
// O que continua a precisar do cabeçalho: a pausa AUTOMÁTICA da AWS por reputação, que é do
// mecanismo dos tenants. Enquanto isso não existir, a pausa é nossa — e é para isso que serve o
// SetSendingEnabled abaixo.

// CreateConfigurationSet cria o conjunto do domínio, com métricas de reputação ligadas.
//
// Sem `ReputationMetricsEnabled` o conjunto existe mas não mede nada, e o painel da SES continua a
// dar um número só para todos os clientes — que é exatamente o problema que isto vem resolver.
func (c *Client) CreateConfigurationSet(ctx context.Context, name string) error {
	body, _ := json.Marshal(map[string]any{
		"ConfigurationSetName": name,
		"ReputationOptions":    map[string]any{"ReputationMetricsEnabled": true},
		"SendingOptions":       map[string]any{"SendingEnabled": true},
	})
	status, out, err := c.do(ctx, http.MethodPost, "/v2/email/configuration-sets", body)
	if err != nil {
		return err
	}
	if status == http.StatusOK || status == http.StatusCreated || jaExiste(status, out) {
		return nil
	}
	return fmt.Errorf("ses create configuration set %q: HTTP %d: %s", name, status, out)
}

// SetIdentityConfigurationSet torna o conjunto o predefinido da identidade. É esta chamada que
// dispensa o cabeçalho por mensagem.
func (c *Client) SetIdentityConfigurationSet(ctx context.Context, domain, name string) error {
	body, _ := json.Marshal(map[string]any{"ConfigurationSetName": name})
	status, out, err := c.do(ctx, http.MethodPut,
		"/v2/email/identities/"+domain+"/configuration-set", body)
	if err != nil {
		return err
	}
	if status == http.StatusOK {
		return nil
	}
	return fmt.Errorf("ses set default config set de %q: HTTP %d: %s", domain, status, out)
}

// SetSendingEnabled liga ou desliga o envio de UM cliente, sem tocar nos outros nem na conta.
//
// É a ferramenta que faltava: até aqui, perante um cliente a queimar a reputação, as opções eram
// não fazer nada ou suspender a conta inteira.
func (c *Client) SetSendingEnabled(ctx context.Context, name string, enabled bool) error {
	body, _ := json.Marshal(map[string]any{"SendingEnabled": enabled})
	status, out, err := c.do(ctx, http.MethodPut,
		"/v2/email/configuration-sets/"+name+"/sending", body)
	if err != nil {
		return err
	}
	if status == http.StatusOK {
		return nil
	}
	return fmt.Errorf("ses sending=%v em %q: HTTP %d: %s", enabled, name, status, out)
}
