package handlers

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/lyrarma/cloud-api/src/models"
	"golang.org/x/text/language"
)

//go:embed registration_email.json
var registrationEmailJSON []byte

var registrationEmailLanguages = language.NewMatcher([]language.Tag{language.English, language.Russian})

func registrationEmailPayload(email, confirmationKey, confirmationURL, acceptLanguage string) (models.ConfirmationRegistrationRequest, error) {
	link, err := url.Parse(confirmationURL)
	if err != nil || (link.Scheme != "https" && link.Scheme != "http") || link.Hostname() == "" || link.User != nil || link.Fragment != "" {
		return models.ConfirmationRegistrationRequest{}, fmt.Errorf("invalid registration confirmation URL")
	}
	if confirmationKey == "" {
		return models.ConfirmationRegistrationRequest{}, fmt.Errorf("registration confirmation key is empty")
	}
	query := link.Query()
	query.Set("key", confirmationKey)
	link.RawQuery = query.Encode()

	var translations map[string]struct {
		Subject string `json:"subject"`
		Text    string `json:"text"`
	}
	if err := json.Unmarshal(registrationEmailJSON, &translations); err != nil {
		return models.ConfirmationRegistrationRequest{}, fmt.Errorf("decode registration email translations: %w", err)
	}
	_, index := language.MatchStrings(registrationEmailLanguages, acceptLanguage)
	locale := []string{"en", "ru"}[index]
	text := translations[locale]
	if text.Subject == "" || !strings.Contains(text.Text, "{confirmation_url}") {
		return models.ConfirmationRegistrationRequest{}, fmt.Errorf("registration email translation is incomplete")
	}
	return models.ConfirmationRegistrationRequest{
		Email: email, Subject: text.Subject,
		Text: strings.ReplaceAll(text.Text, "{confirmation_url}", link.String()),
	}, nil
}
