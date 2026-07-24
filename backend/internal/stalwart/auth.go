package stalwart

import (
	"context"
	"net/http"
)

// VerifyUser confirma a credencial de um usuário FINAL (mailbox) fazendo uma sessão JMAP com
// Basic auth. Usado para autenticar o webmail na NOSSA API sem guardar a senha do usuário
// (DIVERGENCES §D7: Basic auth é o caminho viável na 0.16). Retorna true se HTTP 200.
func (c *Client) VerifyUser(ctx context.Context, email, password string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/jmap/session", nil)
	if err != nil {
		return false, err
	}
	req.SetBasicAuth(email, password)
	resp, err := c.http.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK, nil
}
