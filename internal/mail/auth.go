package mail

import (
	"fmt"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-sasl"

	"mail-manager/internal/config"
)

type xoauth2Client struct {
	username string
	token    string
}

func (a *xoauth2Client) Start() (string, []byte, error) {
	response := "user=" + a.username +
		"\x01auth=Bearer " + a.token + "\x01\x01"

	return "XOAUTH2", []byte(response), nil
}

func (a *xoauth2Client) Next(challenge []byte) ([]byte, error) {
	return []byte{}, nil
}

func authenticate(
	client *imapclient.Client,
	cfg config.MailConfig,
) error {
	if cfg.Username == "" {
		return fmt.Errorf("MAIL_USERNAME is required")
	}

	switch cfg.AuthMethod {
	case "oauth2":
		if cfg.OAuthToken == "" {
			return fmt.Errorf("MAIL_OAUTH_TOKEN is required")
		}

		if !client.Caps().Has(imap.AuthCap("XOAUTH2")) {
			return fmt.Errorf("IMAP server does not advertise XOAUTH2")
		}

		return client.Authenticate(&xoauth2Client{
			username: cfg.Username,
			token:    cfg.OAuthToken,
		})

	case "app_password":
		if cfg.AppPassword == "" {
			return fmt.Errorf("MAIL_APP_PASSWORD is required")
		}

		return client.Login(
			cfg.Username,
			cfg.AppPassword,
		).Wait()

	default:
		return fmt.Errorf(
			"unsupported auth method: %s",
			cfg.AuthMethod,
		)
	}
}

var _ sasl.Client = (*xoauth2Client)(nil)
