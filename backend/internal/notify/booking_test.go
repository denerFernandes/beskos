package notify

import (
	"strings"
	"testing"
)

func TestBookingConfirmedFollowsTheHostLanguage(t *testing.T) {
	subjectPT, bodyPT := BookingConfirmed("pt-BR", "Weekly sync", "Ana", "02/01/2006 15:04", 30, "https://beskos.com/j/room")
	if !strings.Contains(subjectPT, "confirmada") || !strings.Contains(bodyPT, "confirmada") {
		t.Errorf("pt-BR subject/body = %q / %q, want the Portuguese wording", subjectPT, bodyPT)
	}
	if !strings.Contains(bodyPT, "https://beskos.com/j/room") {
		t.Error("the meeting link must be in the body")
	}

	subjectEN, bodyEN := BookingConfirmed("en", "Weekly sync", "Ana", "02/01/2006 15:04", 30, "")
	if !strings.Contains(subjectEN, "confirmed") || !strings.Contains(bodyEN, "confirmed") {
		t.Errorf("en subject/body = %q / %q, want the English wording", subjectEN, bodyEN)
	}
	if strings.Contains(bodyEN, "Video call") {
		t.Error("an empty meetURL must not add a video-call line")
	}
}
