package classifier

import "testing"

func TestPolicy(t *testing.T) {
	cases := []struct {
		d               Decision
		include, review bool
	}{
		{Decision{SpamVerdict: "spam", Category: "promotion", Importance: "low", ActionType: "none", Summary: "Очевидный спам", Reason: "Массовое предложение"}, false, false},
		{Decision{SpamVerdict: "not_spam", Category: "security", Importance: "high", ActionRequired: true, ActionType: "security", Summary: "Предупреждение", Reason: "Изменение пароля"}, true, false},
		{Decision{SpamVerdict: "uncertain", Category: "finance", Importance: "uncertain", ActionType: "none", Summary: "Требует проверки", Reason: "Неясно, настоящий ли отправитель"}, true, true},
	}
	for _, tt := range cases {
		if err := Validate(tt.d); err != nil {
			t.Fatal(err)
		}
		if IncludeInReport(tt.d) != tt.include || NeedsReview(tt.d, false) != tt.review {
			t.Fatalf("policy mismatch for %+v", tt.d)
		}
	}
}
func TestInvalidDecision(t *testing.T) {
	d := Decision{SpamVerdict: "not_spam", Category: "work", Importance: "low", ActionRequired: true, ActionType: "none", Summary: "Тест", Reason: "Тест"}
	if Validate(d) == nil {
		t.Fatal("missing action_type accepted")
	}
	d.ActionType = "reply"
	d.SpamVerdict = "spam"
	if Validate(d) == nil {
		t.Fatal("spam action accepted")
	}
}
