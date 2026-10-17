// Package classifier owns the stable classification taxonomy used by both
// the model and the React API. The application, not the model, determines reports.
package classifier

import (
	"fmt"
	"strings"
)

var Categories = []string{"personal", "work", "finance", "security", "shopping", "delivery", "account", "social", "newsletter", "promotion", "notification", "other", "uncertain"}
var Importances = []string{"low", "medium", "high", "uncertain"}
var SpamVerdicts = []string{"spam", "not_spam", "uncertain"}
var ActionTypes = []string{"none", "reply", "review", "payment", "deadline", "security", "other"}

type Decision struct {
	SpamVerdict    string `json:"spam_verdict"`
	Category       string `json:"category"`
	Importance     string `json:"importance"`
	ActionRequired bool   `json:"action_required"`
	ActionType     string `json:"action_type"`
	Summary        string `json:"summary"`
	Reason         string `json:"reason"`
}

func valid(value string, choices []string) bool {
	for _, choice := range choices {
		if choice == value {
			return true
		}
	}
	return false
}
func Validate(d Decision) error {
	if !valid(d.SpamVerdict, SpamVerdicts) {
		return fmt.Errorf("invalid spam_verdict")
	}
	if !valid(d.Category, Categories) {
		return fmt.Errorf("invalid category")
	}
	if !valid(d.Importance, Importances) {
		return fmt.Errorf("invalid importance")
	}
	if !valid(d.ActionType, ActionTypes) {
		return fmt.Errorf("invalid action_type")
	}
	if len([]rune(strings.TrimSpace(d.Summary))) == 0 || len([]rune(d.Summary)) > 240 {
		return fmt.Errorf("summary must be 1..240 characters")
	}
	if len([]rune(strings.TrimSpace(d.Reason))) == 0 || len([]rune(d.Reason)) > 320 {
		return fmt.Errorf("reason must be 1..320 characters")
	}
	if !d.ActionRequired && d.ActionType != "none" {
		return fmt.Errorf("action_type must be none if no action is required")
	}
	if d.ActionRequired && d.ActionType == "none" {
		return fmt.Errorf("action_type required")
	}
	if d.SpamVerdict == "spam" && d.ActionRequired {
		return fmt.Errorf("spam must not require an automatic action")
	}
	return nil
}

// Report policy is deterministic and errs toward including uncertain messages.
func IncludeInReport(d Decision) bool {
	if d.SpamVerdict == "uncertain" {
		return true
	}
	if d.SpamVerdict == "spam" {
		return false
	}
	return d.ActionRequired || d.Importance == "high" || d.Importance == "uncertain" || d.Category == "security" || d.Category == "finance"
}
func NeedsReview(d Decision, truncated bool) bool {
	return truncated || d.Importance == "uncertain" || d.SpamVerdict == "uncertain" || d.Category == "uncertain"
}

// These labels are API presentation hints, not model output.
var CategoryLabelsRU = map[string]string{
	"personal": "Личное", "work": "Работа", "finance": "Финансы",
	"security": "Безопасность", "shopping": "Покупки", "delivery": "Доставка",
	"account": "Аккаунты", "social": "Соцсети", "newsletter": "Рассылки",
	"promotion": "Реклама", "notification": "Уведомления", "other": "Прочее",
	"uncertain": "Не определено",
}
var ImportanceLabelsRU = map[string]string{"low": "Низкая", "medium": "Средняя", "high": "Высокая", "uncertain": "Требует проверки"}
var SpamLabelsRU = map[string]string{"spam": "Спам", "not_spam": "Не спам", "uncertain": "Требует проверки"}
var ActionLabelsRU = map[string]string{"none": "Не требуется", "reply": "Ответить", "review": "Проверить", "payment": "Оплатить", "deadline": "Срок/задача", "security": "Безопасность", "other": "Другое действие"}
