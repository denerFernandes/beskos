// Package config carrega configuração via ambiente. Segredos NUNCA no código (brief §10).
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	HTTPAddr    string
	DatabaseURL string
	RedisURL    string // pub/sub do chat em tempo real (opcional; sem ele o chat fica só REST)

	// Admin GLOBAL do Stalwart — só o backend usa. Nunca exposto ao frontend (brief §2).
	StalwartURL      string
	StalwartAdminID  string
	StalwartAdminPwd string

	// Hostname/domínio default usados no bootstrap do Stalwart (DIVERGENCES §D3).
	StalwartHostname      string
	StalwartDefaultDomain string

	// OnlyOffice Document Server (edição de office). OnlyOfficeURL = URL que o BROWSER usa
	// (api.js + editor); FilesHostURL = URL da NOSSA API como o Document Server a alcança
	// (download/callback). OnlyOfficeJWTSecret é compartilhado com o DS (assina o config);
	// DocTokenSecret assina, internamente, as URLs de download/callback. Segredos via env.
	// OnlyOfficeSourceURL = endereço público do fonte modificado do Document Server. Corremos a
	// imagem oficial com um patch de branding (deploy/onlyoffice/), e a AGPL v3 §13 obriga a
	// oferecer esse fonte a quem usa o editor pela rede. Aparece no "Sobre".
	OnlyOfficeURL         string
	OnlyOfficeInternalURL string
	FilesHostURL          string
	OnlyOfficeJWTSecret   string
	OnlyOfficeSourceURL   string
	DocTokenSecret        string

	// Meet (LiveKit). LiveKitURL = WebSocket do browser; chave/segredo assinam os tokens de sala.
	// LiveKitHTTPURL = API Twirp (nosso Go → LiveKit) para gravação via Egress.
	LiveKitURL       string
	LiveKitHTTPURL   string
	LiveKitAPIKey    string
	LiveKitAPISecret string

	// Notificações por e-mail (opcional). Sem NOTIFY_FROM/HOST, o envio fica desligado.
	NotifySMTPHost     string
	NotifySMTPPort     string
	NotifyFrom         string
	NotifySMTPUser     string
	NotifySMTPPassword string
	NotifySMTPInsecure bool

	// Web Push (notificações desktop com o Beskos fechado). Sem VAPID_PRIVATE_KEY o push fica
	// desligado (as notificações com aba aberta continuam a funcionar no cliente). O par de chaves
	// VAPID gera-se uma vez; o subject é um mailto: de contacto exigido pelo protocolo.
	VAPIDPublicKey  string
	VAPIDPrivateKey string
	VAPIDSubject    string

	// Tamanho máximo por ficheiro no upload para o Files. Configurável (FILES_MAX_UPLOAD_BYTES, em
	// bytes); default 100 GiB. A cota total do plano continua a valer por cima disto.
	FilesMaxUploadBytes int64

	// App nativo (Tauri): origens permitidas para CORS (APP_ORIGINS, separadas por vírgula) e se o
	// cookie de sessão é Secure/SameSite=None (COOKIE_SECURE=true em produção HTTPS).
	AppOrigins   []string
	CookieSecure bool

	// URL pública da app (PUBLIC_URL, ex.: https://app.beskos.com), usada para montar links
	// absolutos que saem para fora — como o link de uma sala de reunião num e-mail. Vazio → os
	// links ficam relativos (útil em dev; num e-mail seriam inúteis).
	PublicURL string

	// Environment: "development" (default) ou "production". Em produção a config falha fechado —
	// recusa segredos vazios, fracos ou defaults de dev conhecidos (auditoria H8/H10).
	Environment string

	// MFAEncKey cifra o segredo TOTP em repouso (AES-GCM). Vazio = guarda em claro (dev).
	MFAEncKey string
}

// IsProduction indica se estamos em produção (APP_ENV=production).
func (c *Config) IsProduction() bool { return c.Environment == "production" }

// knownDevSecrets são valores default de desenvolvimento que NUNCA podem ir para produção.
var knownDevSecrets = map[string]bool{
	"dev-oo-jwt-secret-change-me":            true,
	"dev-doc-token-secret-change-me":         true,
	"devkey":                                 true,
	"devsecret_livekit_change_me_0123456789": true,
	"dev-admin-secret":                       true,
	"devpass":                                true,
	"devminio123":                            true,
	"changeme":                               true,
}

// defaultOfficeSourceURL é onde publicamos o fonte do Document Server modificado que servimos —
// a oferta que a AGPL v3 §13 exige. Não é segredo nem varia por ambiente, e esquecê-lo derrubava o
// arranque em produção, por isso vem por omissão em vez de ser um passo manual no painel.
// ATENÇÃO: quem alterar o patch de branding TEM de apontar isto (ou a env) para o seu próprio
// repositório — senão o editor promete aos utilizadores um fonte que não corresponde ao que corre.
const defaultOfficeSourceURL = "https://github.com/denerFernandes/beskos-docs-server"

func Load() (*Config, error) {
	c := &Config{
		HTTPAddr:              getenv("HTTP_ADDR", ":8000"),
		DatabaseURL:           os.Getenv("DATABASE_URL"),
		RedisURL:              getenv("REDIS_URL", ""),
		StalwartURL:           getenv("STALWART_URL", "http://localhost:8080"),
		StalwartAdminID:       getenv("STALWART_ADMIN_ID", "admin"),
		StalwartAdminPwd:      os.Getenv("STALWART_ADMIN_SECRET"),
		StalwartHostname:      getenv("STALWART_HOSTNAME", "mail.localhost"),
		StalwartDefaultDomain: getenv("STALWART_DEFAULT_DOMAIN", "localhost"),
		OnlyOfficeURL:         getenv("ONLYOFFICE_URL", ""),
		OnlyOfficeInternalURL: getenv("ONLYOFFICE_INTERNAL_URL", ""),
		FilesHostURL:          getenv("FILES_HOST_URL", ""),
		OnlyOfficeJWTSecret:   os.Getenv("ONLYOFFICE_JWT_SECRET"),
		OnlyOfficeSourceURL:   strings.TrimRight(getenv("ONLYOFFICE_SOURCE_URL", defaultOfficeSourceURL), "/"),
		DocTokenSecret:        os.Getenv("DOC_TOKEN_SECRET"),
		LiveKitURL:            getenv("LIVEKIT_URL", ""),
		LiveKitHTTPURL:        getenv("LIVEKIT_HTTP_URL", ""),
		LiveKitAPIKey:         getenv("LIVEKIT_API_KEY", ""),
		LiveKitAPISecret:      os.Getenv("LIVEKIT_API_SECRET"),
		NotifySMTPHost:        getenv("NOTIFY_SMTP_HOST", ""),
		NotifySMTPPort:        getenv("NOTIFY_SMTP_PORT", "587"),
		// Remetente dos avisos ao cliente. O endereço tem de existir como caixa no Stalwart (ou ser
		// aceite pelo SMTP configurado) — senão o servidor recusa o envio.
		NotifyFrom:          getenv("NOTIFY_FROM", "Beskos <noreply@beskos.com>"),
		NotifySMTPUser:      getenv("NOTIFY_SMTP_USER", ""),
		NotifySMTPPassword:  os.Getenv("NOTIFY_SMTP_PASSWORD"),
		NotifySMTPInsecure:  getenv("NOTIFY_SMTP_INSECURE", "false") == "true",
		VAPIDPublicKey:      getenv("VAPID_PUBLIC_KEY", ""),
		VAPIDPrivateKey:     os.Getenv("VAPID_PRIVATE_KEY"),
		VAPIDSubject:        getenv("VAPID_SUBJECT", "mailto:admin@localhost"),
		FilesMaxUploadBytes: getenvInt64("FILES_MAX_UPLOAD_BYTES", 100<<30), // 100 GiB
		AppOrigins:          splitList(getenv("APP_ORIGINS", "")),
		CookieSecure:        getenv("COOKIE_SECURE", "false") == "true",
		PublicURL:           strings.TrimRight(getenv("PUBLIC_URL", ""), "/"),
		Environment:         getenv("APP_ENV", "development"),
		MFAEncKey:           os.Getenv("MFA_ENC_KEY"),
	}
	if c.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	if c.IsProduction() {
		if err := c.validateProduction(); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// validateProduction recusa o boot quando um segredo obrigatório de uma feature ligada está vazio,
// fraco ou é um default de dev conhecido — em vez de subir num estado inseguro (auditoria H8/H10).
func (c *Config) validateProduction() error {
	var errs []string
	// checkSecret exige segredo forte (≥16 chars) e não-default quando `enabled`.
	checkSecret := func(name, val string, enabled bool) {
		if !enabled {
			return
		}
		switch {
		case val == "":
			errs = append(errs, name+" is required in production")
		case knownDevSecrets[val]:
			errs = append(errs, name+" uses a known dev default — set a real secret")
		case len(val) < 16:
			errs = append(errs, name+" is too short (need ≥16 chars)")
		}
	}
	checkSecret("STALWART_ADMIN_SECRET", c.StalwartAdminPwd, true)
	// Cifra a credencial JMAP e o segredo TOTP em repouso — obrigatória em produção (brokering H11).
	checkSecret("MFA_ENC_KEY", c.MFAEncKey, true)
	checkSecret("ONLYOFFICE_JWT_SECRET", c.OnlyOfficeJWTSecret, c.OnlyOfficeURL != "")
	checkSecret("DOC_TOKEN_SECRET", c.DocTokenSecret, c.OnlyOfficeURL != "")
	checkSecret("LIVEKIT_API_SECRET", c.LiveKitAPISecret, c.LiveKitURL != "")
	// Conformidade AGPL §13: servimos um Document Server modificado, logo temos de dizer aos
	// utilizadores onde está o fonte dessas modificações. Sem endereço, não subimos em produção.
	if c.OnlyOfficeURL != "" && c.OnlyOfficeSourceURL == "" {
		errs = append(errs, "ONLYOFFICE_SOURCE_URL is required in production (AGPL v3 §13: publish the modified Document Server source)")
	}
	if c.LiveKitURL != "" && knownDevSecrets[c.LiveKitAPIKey] {
		errs = append(errs, "LIVEKIT_API_KEY uses a known dev default — set a real key")
	}
	if c.VAPIDPrivateKey != "" && knownDevSecrets[c.VAPIDPrivateKey] {
		errs = append(errs, "VAPID_PRIVATE_KEY uses a known dev default")
	}
	// Higiene de transporte em produção.
	if !c.CookieSecure {
		errs = append(errs, "COOKIE_SECURE must be true in production (session cookie over HTTPS)")
	}
	if c.NotifySMTPInsecure {
		errs = append(errs, "NOTIFY_SMTP_INSECURE must be false in production (TLS verification)")
	}
	if len(errs) > 0 {
		return fmt.Errorf("insecure production config: %s", strings.Join(errs, "; "))
	}
	return nil
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// splitList divide "a, b ,c" numa lista limpa (vazio → nil).
func splitList(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func getenvInt64(k string, def int64) int64 {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return def
}
