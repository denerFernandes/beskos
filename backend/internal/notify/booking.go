package notify

import "fmt"

// BookingConfirmed builds the subject and plain-text body sent to whoever just booked a meeting
// slot on a public booking page. The booker has no stored language preference (they are not a
// registered user), so this follows the host's language -- the same fallback the rest of this
// package uses when the recipient's own preference isn't known.
func BookingConfirmed(lang, title, who, when string, durationMin int, meetURL string) (subject, body string) {
	subjectPT := "Reunião confirmada: " + title // pt-BR subject line -- lang:ok
	subject = tr(lang, subjectPT, "Meeting confirmed: "+title)
	bodyPT := "A sua reunião com %s está confirmada.\r\nQuando: %s\r\nDuração: %d min\r\n" // lang:ok
	bodyEN := "Your meeting with %s is confirmed.\r\nWhen: %s\r\nDuration: %d min\r\n"
	body = fmt.Sprintf(tr(lang, bodyPT, bodyEN), who, when, durationMin)
	if meetURL != "" {
		videoLabelPT := "Videochamada: " // lang:ok
		body += tr(lang, videoLabelPT, "Video call: ") + meetURL + "\r\n"
	}
	return subject, body
}
