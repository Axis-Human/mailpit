package smtpd

import (
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"testing"
	"time"

	"github.com/axllent/mailpit/config"
	"github.com/axllent/mailpit/internal/logger"
	"github.com/axllent/mailpit/internal/storage"
)

func freeAddr(t *testing.T) string {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()

	return l.Addr().String()
}

func testMessage(subject string) []byte {
	return fmt.Appendf(nil, "From: from@example.com\r\nTo: to@example.com\r\nSubject: %s\r\nMessage-ID: <%s@example.com>\r\n\r\nBody\r\n", subject, subject)
}

func TestMultiProjectSMTP(t *testing.T) {
	logger.NoLogging = true
	config.MultiProject = true
	config.SMTPAuthAllowInsecure = true
	config.MaxMessages = 0
	config.Database = ""

	t.Cleanup(func() {
		storage.Close()
		config.MultiProject = false
		config.SMTPAuthAllowInsecure = false
	})

	if err := storage.InitDB(); err != nil {
		t.Fatal(err)
	}

	a, keyA, err := storage.CreateProject("SMTP A", "smtp-a")
	if err != nil {
		t.Fatal(err)
	}
	b, keyB, err := storage.CreateProject("SMTP B", "smtp-b")
	if err != nil {
		t.Fatal(err)
	}

	addr := freeAddr(t)
	go func() { _ = listenAndServe(addr, mailHandler, authHandler) }()

	for range 100 {
		if c, err := net.Dial("tcp", addr); err == nil {
			_ = c.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	send := func(user, pass, subject string) error {
		return smtp.SendMail(addr, smtp.PlainAuth("", user, pass, "127.0.0.1"), "from@example.com", []string{"to@example.com"}, testMessage(subject))
	}

	t.Run("messages are stored in the project of the API key", func(t *testing.T) {
		if err := send("smtp-a", keyA, "one"); err != nil {
			t.Fatal(err)
		}
		// the username is only a label
		if err := send("anything", keyB, "two"); err != nil {
			t.Fatal(err)
		}
		if err := send("", keyB, "three"); err != nil {
			t.Fatal(err)
		}

		if got := storage.CountTotal(a.ID); got != 1 {
			t.Fatalf("project A has %d messages, expected 1", got)
		}
		if got := storage.CountTotal(b.ID); got != 2 {
			t.Fatalf("project B has %d messages, expected 2", got)
		}
		if got := storage.CountTotal(""); got != 0 {
			t.Fatalf("the single mailbox has %d messages, expected 0", got)
		}
	})

	t.Run("invalid credentials are refused", func(t *testing.T) {
		for _, key := range []string{"wrong", "mpk_wrong", "", strings.ToUpper(keyA)} {
			err := send("smtp-a", key, "refused")
			if err == nil || !strings.Contains(err.Error(), "535") {
				t.Fatalf("API key %q should be refused with 535, got %v", key, err)
			}
		}
	})

	t.Run("authentication is required", func(t *testing.T) {
		err := smtp.SendMail(addr, nil, "from@example.com", []string{"to@example.com"}, testMessage("anonymous"))
		if err == nil || !strings.Contains(err.Error(), "530") {
			t.Fatalf("sending without authentication should be refused with 530, got %v", err)
		}
	})

	t.Run("disabled projects reject new connections and messages", func(t *testing.T) {
		// connect & authenticate while the project is enabled
		client, err := smtp.Dial(addr)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = client.Close() }()

		if err := client.Auth(smtp.PlainAuth("", "smtp-a", keyA, "127.0.0.1")); err != nil {
			t.Fatal(err)
		}

		disabled := true
		if _, err := storage.UpdateProject(a.ID, storage.ProjectUpdate{Disabled: &disabled}); err != nil {
			t.Fatal(err)
		}

		// a new connection cannot authenticate, and says why
		err = send("smtp-a", keyA, "disabled")
		if err == nil || !strings.Contains(err.Error(), "535") || !strings.Contains(err.Error(), "disabled") {
			t.Fatalf("a disabled project should be refused with 535, got %v", err)
		}

		// the existing connection can no longer deliver messages
		if err := client.Mail("from@example.com"); err != nil {
			t.Fatal(err)
		}
		if err := client.Rcpt("to@example.com"); err != nil {
			t.Fatal(err)
		}
		w, err := client.Data()
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write(testMessage("existing-connection"))
		err = w.Close()
		if err == nil || !strings.Contains(err.Error(), "550") {
			t.Fatalf("a message for a disabled project should be refused with 550, got %v", err)
		}

		if got := storage.CountTotal(a.ID); got != 1 {
			t.Fatalf("project A has %d messages, expected 1", got)
		}

		// other projects are not affected, and enabling the project works again
		if err := send("smtp-b", keyB, "four"); err != nil {
			t.Fatal(err)
		}

		disabled = false
		if _, err := storage.UpdateProject(a.ID, storage.ProjectUpdate{Disabled: &disabled}); err != nil {
			t.Fatal(err)
		}
		if err := send("smtp-a", keyA, "enabled"); err != nil {
			t.Fatal(err)
		}
		if got := storage.CountTotal(a.ID); got != 2 {
			t.Fatalf("project A has %d messages, expected 2", got)
		}
	})

	t.Run("rotating the key", func(t *testing.T) {
		newKey, err := storage.RotateProjectAPIKey(a.ID)
		if err != nil {
			t.Fatal(err)
		}

		if err := send("smtp-a", keyA, "old-key"); err == nil {
			t.Fatal("the old key still works")
		}
		if err := send("smtp-a", newKey, "new-key"); err != nil {
			t.Fatal(err)
		}
	})
}
