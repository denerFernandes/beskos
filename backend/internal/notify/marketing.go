package notify

import "html"

// WaitlistInvite tells somebody on the waiting list that there is room for them.
//
// It is the first thing we ever send to a person who is not yet a customer, so it does one job and
// stops: says the wait is over, gives the link, and says what happens next. No selling -- they
// already asked, and being sold to after asking is how a list becomes a spam complaint.
func WaitlistInvite(lang, name, signupURL string) Msg {
	subject := tr(lang, "O seu lugar no Beskos está disponível", "Your place on Beskos is ready") // lang:ok
	greeting := ""
	if name != "" {
		greeting = tr(lang, "Estimado(a) ", "Dear ") + html.EscapeString(name) + ", " // lang:ok
	}
	intro := greeting + tr(lang,
		"há lugar disponível para a sua organização. O registo pode ser feito na ligação abaixo, e a migração do correio do fornecedor anterior é feita a partir da própria consola.", // lang:ok
		"a place is now available for your organisation. The account can be created from the link below, and mail is imported from the previous provider inside the console itself.")
	footer := tr(lang,
		"Recebeu esta mensagem porque pediu para ser avisado quando houvesse lugar. Se já não tiver interesse, basta ignorá-la.", // lang:ok
		"You're getting this because you asked to be told when a place opened. If you're no longer interested, simply ignore it.")
	return build(lang, subject, intro, nil,
		tr(lang, "Criar a conta", "Create the account"), signupURL, footer) // lang:ok
}

// ContactEnquiry is a message from the contact form on the marketing site, addressed to the sales
// mailbox. It is read by us, not by a customer, so it is plain and complete rather than branded:
// every field the person filled in, in the order it helps to answer them.
func ContactEnquiry(name, email, company, locale, message string) Msg {
	subject := "Contact form: " + name
	if company != "" {
		subject += " (" + company + ")"
	}
	lines := []Line{{"Name", name}, {"E-mail", email}}
	if company != "" {
		lines = append(lines, Line{"Organisation", company})
	}
	if locale != "" {
		// Which language they were reading the site in, so the reply goes out in it.
		lines = append(lines, Line{"Language", locale})
	}
	body := "<p style=\"white-space:pre-wrap\">" + html.EscapeString(message) + "</p>"
	msg := build("en", subject, body, lines, "", "", "Sent by the contact form on the website.")
	msg.Text = message + "\n\n" + msg.Text
	return msg
}
