package notify

import (
	"fmt"
	"html"
	"strings"
	"time"
)

// Avisos transacionais ao cliente: conta criada, subscrição, teste, cobrança e novo início de
// sessão. São o único contacto que a pessoa tem connosco fora da app — por isso vão com a marca,
// no idioma dela, e sempre com a versão em texto simples ao lado (clientes que recusam HTML,
// leitores de ecrã, e o filtro de spam que gosta de ver as duas partes).
//
// O idioma segue o do utilizador: "en" quando começa por en, senão português. Não inventamos mais
// idiomas do que a app tem.

// Msg é um aviso pronto a enviar.
type Msg struct {
	Subject string
	HTML    string
	Text    string
}

// Line é uma linha de detalhe do aviso (rótulo → valor), mostrada numa tabela discreta.
type Line struct{ Label, Value string }

const brandColor = "#116466"

// isEN diz se a mensagem sai em inglês.
func isEN(lang string) bool { return strings.HasPrefix(strings.ToLower(lang), "en") }

// tr escolhe entre o texto português e o inglês.
func tr(lang, pt, en string) string {
	if isEN(lang) {
		return en
	}
	return pt
}

// build monta o HTML da marca + a versão em texto. `cta`/`ctaURL` vazios = aviso sem botão.
func build(lang, subject, intro string, lines []Line, cta, ctaURL, footer string) Msg {
	var rows, txt strings.Builder
	for _, l := range lines {
		rows.WriteString(fmt.Sprintf(
			`<tr><td style="padding:7px 0;color:#6C7370;font-size:14px">%s</td>`+
				`<td style="padding:7px 0;text-align:right;font-size:14px;font-weight:600;color:#10161A">%s</td></tr>`,
			html.EscapeString(l.Label), html.EscapeString(l.Value)))
		txt.WriteString(l.Label + ": " + l.Value + "\n")
	}
	button := ""
	if cta != "" && ctaURL != "" {
		button = fmt.Sprintf(
			`<a href="%s" style="display:inline-block;margin-top:22px;padding:13px 26px;background:%s;`+
				`color:#fff;border-radius:10px;text-decoration:none;font-weight:600;font-size:15px">%s</a>`,
			html.EscapeString(ctaURL), brandColor, html.EscapeString(cta))
	}
	body := fmt.Sprintf(`<!doctype html>
<html><body style="margin:0;padding:0;background:#FBFAF8">
<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" style="background:#FBFAF8;padding:32px 16px">
<tr><td align="center">
<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" style="max-width:480px;background:#fff;border:1px solid #E7E3DC;border-radius:16px;padding:32px;font-family:-apple-system,Segoe UI,Roboto,sans-serif">
<tr><td>
<div style="font-size:18px;font-weight:700;color:#10161A;letter-spacing:-0.01em">Beskos</div>
<h1 style="margin:22px 0 10px;font-size:21px;line-height:1.3;color:#10161A;letter-spacing:-0.02em">%s</h1>
<p style="margin:0;color:#4A5250;font-size:15px;line-height:1.6">%s</p>
%s
%s
<p style="margin:26px 0 0;color:#9BA29F;font-size:12px;line-height:1.6">%s</p>
</td></tr></table>
</td></tr></table>
</body></html>`,
		html.EscapeString(subject), intro,
		func() string {
			if rows.Len() == 0 {
				return ""
			}
			return `<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="margin-top:20px;border-top:1px solid #E7E3DC">` + rows.String() + `</table>`
		}(),
		button, html.EscapeString(footer))

	plain := stripTags(intro) + "\n\n" + txt.String()
	if ctaURL != "" {
		plain += "\n" + cta + ": " + ctaURL + "\n"
	}
	plain += "\n" + footer + "\n"
	return Msg{Subject: subject, HTML: body, Text: plain}
}

// stripTags tira o pouco HTML que os textos usam (só <b>), para a parte em texto simples.
func stripTags(s string) string {
	r := strings.NewReplacer("<b>", "", "</b>", "", "<br>", "\n")
	return r.Replace(s)
}

func footerFor(lang string) string {
	return tr(lang,
		"Recebeu esta mensagem porque tem uma conta Beskos. Não responda a este endereço.",
		"You're getting this because you have a Beskos account. Please don't reply to this address.")
}

// AccountCreated: a conta existe e o endereço já é o da pessoa.
func AccountCreated(lang, email, domain, appURL string) Msg {
	subject := tr(lang, "A sua conta Beskos está pronta", "Your Beskos account is ready")
	intro := tr(lang,
		"Criámos a sua organização e a sua caixa de correio. Entre e defina a sua palavra-passe.",
		"We've created your organisation and your mailbox. Sign in and set your password.")
	lines := []Line{{tr(lang, "Endereço", "Address"), email}, {tr(lang, "Domínio", "Domain"), domain}}
	footer := tr(lang,
		"Para receber e-mail de fora, publique os registos de DNS do domínio — a app mostra-lhe quais. "+footerFor(lang),
		"To receive outside email, publish the domain's DNS records — the app shows you which. "+footerFor(lang))
	return build(lang, subject, intro, lines, tr(lang, "Entrar no Beskos", "Sign in to Beskos"), appURL, footer)
}

// TrialStarted: cartão registado, teste a correr, e a data em que começa a pagar.
func TrialStarted(lang, plan string, days int, endsAt time.Time, amount, appURL string) Msg {
	subject := tr(lang,
		fmt.Sprintf("O seu teste de %d dias começou", days),
		fmt.Sprintf("Your %d-day trial has started", days))
	intro := tr(lang,
		"Está tudo a funcionar. Não cobramos nada até ao fim do teste, e pode cancelar antes sem pagar.",
		"Everything is running. We charge nothing until the trial ends, and you can cancel before without paying.")
	lines := []Line{
		{tr(lang, "Plano", "Plan"), plan},
		{tr(lang, "Teste até", "Trial until"), endsAt.Format("02/01/2006")},
		{tr(lang, "Depois", "Then"), amount},
	}
	return build(lang, subject, intro, lines, tr(lang, "Abrir o Beskos", "Open Beskos"), appURL, footerFor(lang))
}

// SubscriptionActive: a subscrição ficou ativa (checkout concluído).
func SubscriptionActive(lang, plan, cycle, amount, appURL string) Msg {
	subject := tr(lang, "Subscrição confirmada", "Subscription confirmed")
	intro := tr(lang,
		"A sua subscrição está ativa. A fatura segue para o e-mail de faturação que nos indicou.",
		"Your subscription is active. The invoice goes to the billing email you gave us.")
	lines := []Line{
		{tr(lang, "Plano", "Plan"), plan},
		{tr(lang, "Periodicidade", "Billing period"), cycle},
		{tr(lang, "Valor", "Amount"), amount},
	}
	return build(lang, subject, intro, lines, tr(lang, "Ver a faturação", "View billing"), appURL, footerFor(lang))
}

// ChargeStarted: primeira cobrança (fim do teste) ou renovação paga.
func ChargeStarted(lang, plan, amount, invoiceURL, appURL string) Msg {
	subject := tr(lang, "Pagamento recebido", "Payment received")
	intro := tr(lang,
		"O teste terminou e a subscrição passou a ser cobrada. Obrigado por continuar connosco.",
		"The trial has ended and the subscription is now being charged. Thanks for staying with us.")
	lines := []Line{{tr(lang, "Plano", "Plan"), plan}, {tr(lang, "Valor", "Amount"), amount}}
	url, label := appURL, tr(lang, "Ver a faturação", "View billing")
	if invoiceURL != "" {
		url, label = invoiceURL, tr(lang, "Ver a fatura", "View the invoice")
	}
	return build(lang, subject, intro, lines, label, url, footerFor(lang))
}

// NewLogin avisa de um início de sessão, com o que permite reconhecê-lo (ou não): quando, de onde,
// com que aparelho. É o aviso que faz alguém perceber que lhe roubaram a senha.
func NewLogin(lang, email, ip, location, device string, when time.Time, appURL string) Msg {
	subject := tr(lang, "Novo início de sessão na sua conta", "New sign-in to your account")
	intro := tr(lang,
		"Houve um início de sessão na sua conta Beskos. Se foi você, não tem de fazer nada.",
		"Someone signed in to your Beskos account. If it was you, there's nothing to do.")
	if location == "" {
		location = tr(lang, "desconhecida", "unknown")
	}
	lines := []Line{
		{tr(lang, "Conta", "Account"), email},
		{tr(lang, "Data e hora", "Date and time"), when.Format("02/01/2006 15:04") + " UTC"},
		{tr(lang, "Endereço IP", "IP address"), ip},
		{tr(lang, "Localização", "Location"), location},
		{tr(lang, "Aparelho", "Device"), device},
	}
	footer := tr(lang,
		"Se não foi você, mude a palavra-passe já e ative a verificação em dois passos. "+footerFor(lang),
		"If this wasn't you, change your password now and turn on two-step verification. "+footerFor(lang))
	return build(lang, subject, intro, lines, tr(lang, "Rever a segurança", "Review security"), appURL, footer)
}

// EventInvite: convite de agenda para alguém de FORA da organização. Quem é de dentro recebe a
// notificação na app e o evento aparece-lhe no calendário — mandar-lhe e-mail seria ruído.
func EventInvite(lang, actor, title, when, location, meetURL string) Msg {
	subject := tr(lang, "Convite: ", "Invitation: ") + title
	intro := tr(lang,
		html.EscapeString(actor)+" convidou-o para um evento.",
		html.EscapeString(actor)+" invited you to an event.")
	lines := []Line{{tr(lang, "Quando", "When"), when}}
	if location != "" {
		lines = append(lines, Line{tr(lang, "Onde", "Where"), location})
	}
	cta, url := "", ""
	if meetURL != "" {
		cta, url = tr(lang, "Entrar na videochamada", "Join the video call"), meetURL
		lines = append(lines, Line{tr(lang, "Videochamada", "Video call"), meetURL})
	}
	footer := tr(lang,
		"Recebeu este convite porque foi indicado como participante.",
		"You're getting this invitation because you were listed as an attendee.")
	return build(lang, subject, intro, lines, cta, url, footer)
}

// FileShared: ficheiro partilhado com alguém de FORA da organização — leva o link público, que é a
// única forma de lá chegar sem conta. Dentro da organização é notificação na app.
func FileShared(lang, actor, fileName, linkURL, expires string) Msg {
	subject := tr(lang, "Partilharam um ficheiro consigo", "A file was shared with you")
	intro := tr(lang,
		html.EscapeString(actor)+" partilhou um ficheiro consigo.",
		html.EscapeString(actor)+" shared a file with you.")
	lines := []Line{{tr(lang, "Ficheiro", "File"), fileName}}
	if expires != "" {
		lines = append(lines, Line{tr(lang, "Link válido até", "Link valid until"), expires})
	}
	footer := tr(lang,
		"O link é pessoal — quem o tiver consegue abrir o ficheiro.",
		"The link is personal — anyone who has it can open the file.")
	return build(lang, subject, intro, lines, tr(lang, "Abrir o ficheiro", "Open the file"), linkURL, footer)
}

// PasswordResetRequested: somebody in the organisation cannot sign in and asked for help. It goes
// to the administrators, who are the ones with the button.
//
// IT CARRIES NO LINK that resets anything: the administrator goes to the panel, where they are
// already signed in. An e-mail that can reset passwords is an e-mail worth stealing.
func PasswordResetRequested(lang, who, name, appURL string) Msg {
	subject := tr(lang, "Pedido de nova palavra-passe", "Password reset requested") // lang:ok
	intro := tr(lang,
		"Alguém da sua organização não consegue entrar e pediu uma palavra-passe nova. Se reconhece o pedido, defina-lha no painel e entregue-a por um canal que já use com essa pessoa.", // lang:ok
		"Somebody in your organisation cannot sign in and asked for a new password. If you recognise the request, set one in the panel and hand it over through a channel you already use with them.")
	lines := []Line{{tr(lang, "Quem pediu", "Who asked"), who}} // lang:ok
	if name != "" {
		lines = append(lines, Line{tr(lang, "Nome", "Name"), name}) // lang:ok
	}
	footer := tr(lang,
		"Se não reconhece este pedido, não faça nada: sem a sua ação, nada muda. "+footerFor(lang), // lang:ok
		"If you do not recognise this request, do nothing: without your action, nothing changes. "+footerFor(lang))
	return build(lang, subject, intro, lines, tr(lang, "Abrir o painel", "Open the panel"), appURL, footer)
}

// PasswordResetLink: the self-service way back, and only for somebody who set an address OUTSIDE
// their own domain -- typically the owner of a small organisation, with no administrator above them.
func PasswordResetLink(lang, account, url string, minutes int) Msg {
	subject := tr(lang, "Repor a palavra-passe do Beskos", "Reset your Beskos password") // lang:ok
	intro := tr(lang,
		"Recebemos um pedido para repor a palavra-passe desta conta. A ligação abaixo serve uma vez.", // lang:ok
		"We received a request to reset the password for this account. The link below works once.")
	lines := []Line{
		{tr(lang, "Conta", "Account"), account},                                   // lang:ok
		{tr(lang, "Válida durante", "Valid for"), fmt.Sprintf("%d min", minutes)}, // lang:ok
	}
	footer := tr(lang,
		"Se não foi você, ignore este e-mail — a palavra-passe atual continua a funcionar. Se a conta tiver segundo fator, ele continua a ser pedido. "+footerFor(lang), // lang:ok
		"If this was not you, ignore this e-mail — the current password still works. If the account has a second factor, it is still required. "+footerFor(lang))
	return build(lang, subject, intro, lines, tr(lang, "Definir uma nova", "Set a new one"), url, footer) // lang:ok
}
