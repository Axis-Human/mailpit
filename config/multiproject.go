package config

import (
	"errors"
	"net/mail"
	"strings"

	"github.com/axllent/mailpit/internal/auth"
	"github.com/axllent/mailpit/internal/logger"
)

// verifyMultiProjectConfig validates the configuration used by the multi-project mode.
// In this mode authentication is handled by projects & users, so the global credential
// files / "accept any" switches cannot be combined with it.
func verifyMultiProjectConfig() error {
	if !MultiProject {
		return nil
	}

	if auth.UICredentials != nil || UIAuthFile != "" {
		return errors.New("[multi-project] --ui-auth-file cannot be used, users log in with their own account")
	}

	if auth.SendAPICredentials != nil || SendAPIAuthFile != "" || SendAPIAuthAcceptAny {
		return errors.New("[multi-project] the Send API authenticates with the project API key, send API credentials cannot be used")
	}

	if auth.SMTPCredentials != nil || SMTPAuthFile != "" || SMTPAuthAcceptAny {
		return errors.New("[multi-project] SMTP authenticates with the project API key, SMTP credentials cannot be used")
	}

	if SMTPTLSCert == "" && !SMTPAuthAllowInsecure {
		return errors.New("[multi-project] SMTP authentication requires STARTTLS or TLS encryption, run with `--smtp-auth-allow-insecure` to allow insecure authentication")
	}

	if !SMTPAuthAllowInsecure && !SMTPRequireTLS {
		// plaintext authentication is only allowed once the connection is encrypted
		SMTPRequireSTARTTLS = true
	}

	SuperAdminEmail = strings.ToLower(strings.TrimSpace(SuperAdminEmail))
	if SuperAdminEmail != "" {
		if a, err := mail.ParseAddress(SuperAdminEmail); err != nil || a.Address != SuperAdminEmail {
			return errors.New("[multi-project] invalid super admin email address")
		}

		if len(SuperAdminPassword) < 8 {
			return errors.New("[multi-project] the super admin password must be at least 8 characters")
		}
	}

	if SessionHours < 1 {
		return errors.New("[multi-project] session hours must be at least 1")
	}

	if auth.POP3Credentials != nil || POP3AuthFile != "" {
		logger.Log().Warn("[multi-project] the POP3 server is not available in multi-project mode")
	}

	return nil
}
