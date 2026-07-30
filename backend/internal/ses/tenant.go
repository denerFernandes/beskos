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
func jaExiste(status int, corpo []byte) bool {
	return status == http.StatusConflict ||
		(status == http.StatusBadRequest && strings.Contains(string(corpo), "AlreadyExists"))
}
