package mail

import (
	"strings"
	"testing"
)

func TestNormalizeQuotes(t *testing.T) {
	got := NormalizeBody("Пожалуйста, пришлите подписанный договор до пятницы, после этого согласуем оплату и время встречи на следующей неделе.\n\nOn Monday October 12 wrote:\n> previous message")
	if strings.Contains(got, "previous message") {
		t.Fatalf("old quote remains: %q", got)
	}
	if !strings.Contains(got, "договор") {
		t.Fatalf("new message removed: %q", got)
	}
}
func TestShortReplyKeepsContext(t *testing.T) {
	got := NormalizeBody("Да\n\nOn Monday October 12 wrote:\n> Вы согласны со встречей?")
	if !strings.Contains(got, "Да") || !strings.Contains(got, "встречей") {
		t.Fatalf("context lost: %q", got)
	}
}
