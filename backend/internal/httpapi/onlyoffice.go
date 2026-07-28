package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Integração com o OnlyOffice Document Server para edição de office (docx/xlsx/pptx).
// O Document Server (container rede-only, com o patch de branding do arranque — ver
// deploy/DIVERGENCES.md) carrega o config
// que ASSINAMOS por JWT; ele então busca o arquivo e devolve o salvo via callback, ambos
// autenticados por um token HMAC nosso ligado a arquivo+usuário+tenant+permissão.

type onlyoffice struct {
	publicURL    string // URL do Document Server que o BROWSER usa (api.js + editor)
	internalURL  string // URL do DS como a NOSSA API o alcança (baixar o arquivo editado no callback)
	filesHostURL string // URL da NOSSA API como o Document Server a alcança (download/callback)
	jwtSecret    []byte // segredo compartilhado com o DS (assina o config; verifica o callback)
	tokenSecret  []byte // segredo interno que assina as URLs de download/callback
	sourceURL    string // fonte do Document Server modificado, mostrado no "Sobre" (AGPL §13)
	brandURL     string // site para onde o logo do cabeçalho aponta
}

// SetOfficeBranding define os endereços que acompanham a marca Beskos no editor: o fonte
// modificado do Document Server (obrigatório em produção, ver config) e o site do logo.
func (s *Server) SetOfficeBranding(sourceURL, brandURL string) {
	if s.office == nil {
		return
	}
	s.office.sourceURL = strings.TrimRight(sourceURL, "/")
	s.office.brandURL = strings.TrimRight(brandURL, "/")
}

// EnableOnlyOffice liga a edição de office. Sem URLs/segredos, fica desligado. internalURL é
// opcional (default = publicURL) e serve quando o DS gera URLs com o host do browser.
func (s *Server) EnableOnlyOffice(publicURL, internalURL, filesHostURL, jwtSecret, tokenSecret string) {
	if publicURL == "" || filesHostURL == "" || jwtSecret == "" || tokenSecret == "" {
		return
	}
	if internalURL == "" {
		internalURL = publicURL
	}
	s.office = &onlyoffice{
		publicURL:    strings.TrimRight(publicURL, "/"),
		internalURL:  strings.TrimRight(internalURL, "/"),
		filesHostURL: strings.TrimRight(filesHostURL, "/"),
		jwtSecret:    []byte(jwtSecret),
		tokenSecret:  []byte(tokenSecret),
	}
}

// officeChrome monta os parâmetros de apresentação do editor — iguais no editor autenticado e no
// visualizador público, para o produto ter uma cara só.
//
// As abas vão para a barra CLARA com sublinhado no separador ativo. O default do Document Server
// (`tabStyle: fill` + `tabBackground: header`) prende-as à faixa escura com o ativo preenchido, que
// é o que fazia o editor parecer mais pesado do que o das imagens oficiais deles. São parâmetros
// grátis (standard branding), não dependem do patch.
// A paleta é nossa: o patch de arranque acrescenta as cores Beskos ao CSS do editor, por isso o
// tema pedido aqui continua a ser o claro/escuro padrão — é o CSS que troca o azul do ONLYOFFICE
// pelo petróleo. Se o patch falhar, o editor fica azul mas funciona.
func officeChrome(dark bool, plugins bool) map[string]any {
	uiTheme := "theme-light"
	if dark {
		uiTheme = "theme-dark"
	}
	return map[string]any{
		"uiTheme": uiTheme,
		// Cabeçalho de dois níveis: logo + acesso rápido (guardar/imprimir/desfazer) e título em
		// cima, abas por baixo. `compactToolbar: false` mantém a fita expandida ao abrir — senão o
		// editor arranca só com as abas e parece "não editável" até se clicar numa.
		"compactHeader":  false,
		"compactToolbar": false,
		"toolbarNoTabs":  false,
		"hideRightMenu":  true,
		"feedback":       false,
		"help":           false,
		"chat":           false,
		"plugins":        plugins,
		"features": map[string]any{
			"tabStyle":      "line",
			"tabBackground": "toolbar",
		},
	}
}

// reachable reescreve uma URL vinda do DS (host do browser) para o host interno alcançável
// pela API, quando diferem.
func (o *onlyoffice) reachable(u string) string {
	if o.internalURL != o.publicURL && strings.HasPrefix(u, o.publicURL) {
		return o.internalURL + strings.TrimPrefix(u, o.publicURL)
	}
	return u
}

// fetchURLAllowed valida a URL do arquivo editado que o callback manda a NOSSA API buscar. O host
// precisa ser exatamente o do Document Server interno — senão o callback (cujo token HMAC vaza em
// logs/Referer) vira um SSRF: apontar `url` para 169.254.169.254/minio/stalwart e gravar a resposta
// como versão do arquivo (auditoria H1). Devolve a URL reescrita (host interno) e se é permitida.
func (o *onlyoffice) fetchURLAllowed(raw string) (string, bool) {
	rewritten := o.reachable(raw)
	pu, err := url.Parse(rewritten)
	if err != nil || (pu.Scheme != "http" && pu.Scheme != "https") || pu.Host == "" {
		return "", false
	}
	iu, err := url.Parse(o.internalURL)
	if err != nil || !strings.EqualFold(pu.Host, iu.Host) {
		return "", false
	}
	return rewritten, true
}

// --- token HMAC das URLs de download/callback (arquivo+usuário+tenant+permissão) ---

type docClaims struct {
	FileID   uuid.UUID
	UserID   uuid.UUID
	TenantID uuid.UUID
	CanWrite bool
	Exp      time.Time
}

func (o *onlyoffice) mintToken(cl docClaims) string {
	write := "0"
	if cl.CanWrite {
		write = "1"
	}
	payload := strings.Join([]string{cl.FileID.String(), cl.UserID.String(), cl.TenantID.String(), write, strconv.FormatInt(cl.Exp.Unix(), 10)}, "|")
	enc := base64.RawURLEncoding
	mac := hmac.New(sha256.New, o.tokenSecret)
	mac.Write([]byte(payload))
	return enc.EncodeToString([]byte(payload)) + "." + enc.EncodeToString(mac.Sum(nil))
}

func (o *onlyoffice) parseToken(tok string) (docClaims, bool) {
	var cl docClaims
	parts := strings.SplitN(tok, ".", 2)
	if len(parts) != 2 {
		return cl, false
	}
	enc := base64.RawURLEncoding
	payload, err := enc.DecodeString(parts[0])
	if err != nil {
		return cl, false
	}
	sig, err := enc.DecodeString(parts[1])
	if err != nil {
		return cl, false
	}
	mac := hmac.New(sha256.New, o.tokenSecret)
	mac.Write(payload)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return cl, false
	}
	f := strings.Split(string(payload), "|")
	if len(f) != 5 {
		return cl, false
	}
	if cl.FileID, err = uuid.Parse(f[0]); err != nil {
		return cl, false
	}
	if cl.UserID, err = uuid.Parse(f[1]); err != nil {
		return cl, false
	}
	if cl.TenantID, err = uuid.Parse(f[2]); err != nil {
		return cl, false
	}
	cl.CanWrite = f[3] == "1"
	exp, err := strconv.ParseInt(f[4], 10, 64)
	if err != nil {
		return cl, false
	}
	cl.Exp = time.Unix(exp, 0)
	if time.Now().After(cl.Exp) {
		return cl, false
	}
	return cl, true
}

// mintAIToken assina um token ESTÁVEL do utilizador para o plugin de IA do OnlyOffice o usar como
// "API key". O plugin corre no iframe do Document Server (outra origem) e não tem o nosso cookie, por
// isso a autorização viaja no Bearer. Leva user+tenant e uma validade longa; só serve o proxy de IA,
// que é limitado pela quota do próprio utilizador — se o token vazar, o abuso é o dele e trava na quota.
func (o *onlyoffice) mintAIToken(userID, tenantID uuid.UUID, exp time.Time) string {
	payload := "ai|" + userID.String() + "|" + tenantID.String() + "|" + strconv.FormatInt(exp.Unix(), 10)
	enc := base64.RawURLEncoding
	mac := hmac.New(sha256.New, o.tokenSecret)
	mac.Write([]byte(payload))
	return enc.EncodeToString([]byte(payload)) + "." + enc.EncodeToString(mac.Sum(nil))
}

// parseAIToken valida o token de IA e devolve user+tenant. ok=false se assinatura inválida ou expirado.
func (o *onlyoffice) parseAIToken(tok string) (userID, tenantID uuid.UUID, ok bool) {
	parts := strings.SplitN(strings.TrimSpace(tok), ".", 2)
	if len(parts) != 2 {
		return uuid.Nil, uuid.Nil, false
	}
	enc := base64.RawURLEncoding
	payload, err := enc.DecodeString(parts[0])
	if err != nil {
		return uuid.Nil, uuid.Nil, false
	}
	sig, err := enc.DecodeString(parts[1])
	if err != nil {
		return uuid.Nil, uuid.Nil, false
	}
	mac := hmac.New(sha256.New, o.tokenSecret)
	mac.Write(payload)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return uuid.Nil, uuid.Nil, false
	}
	f := strings.Split(string(payload), "|")
	if len(f) != 4 || f[0] != "ai" {
		return uuid.Nil, uuid.Nil, false
	}
	if userID, err = uuid.Parse(f[1]); err != nil {
		return uuid.Nil, uuid.Nil, false
	}
	if tenantID, err = uuid.Parse(f[2]); err != nil {
		return uuid.Nil, uuid.Nil, false
	}
	exp, err := strconv.ParseInt(f[3], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return uuid.Nil, uuid.Nil, false
	}
	return userID, tenantID, true
}

// --- JWT HS256 (assina o config e verifica o callback do Document Server) ---

func (o *onlyoffice) jwtSign(payload map[string]any) (string, error) {
	enc := base64.RawURLEncoding
	header := enc.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	signing := header + "." + enc.EncodeToString(body)
	mac := hmac.New(sha256.New, o.jwtSecret)
	mac.Write([]byte(signing))
	return signing + "." + enc.EncodeToString(mac.Sum(nil)), nil
}

// jwtVerify confere a assinatura HS256 e devolve o payload decodificado (para o callback).
func (o *onlyoffice) jwtVerify(tok string) (map[string]any, bool) {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return nil, false
	}
	enc := base64.RawURLEncoding
	mac := hmac.New(sha256.New, o.jwtSecret)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	sig, err := enc.DecodeString(parts[2])
	if err != nil || !hmac.Equal(sig, mac.Sum(nil)) {
		return nil, false
	}
	raw, err := enc.DecodeString(parts[1])
	if err != nil {
		return nil, false
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, false
	}
	return payload, true
}

// --- mapeamento de tipo ---

var officeExts = map[string]bool{
	"docx": true, "doc": true, "odt": true, "rtf": true, "txt": true,
	"xlsx": true, "xls": true, "ods": true, "csv": true,
	"pptx": true, "ppt": true, "odp": true,
}

// documentType do OnlyOffice: "word" | "cell" | "slide".
func documentType(ext string) string {
	switch ext {
	case "xlsx", "xls", "ods", "csv":
		return "cell"
	case "pptx", "ppt", "odp":
		return "slide"
	default:
		return "word"
	}
}

func extOf(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return strings.ToLower(name[i+1:])
	}
	return ""
}
