// Package mail sends the password reset emails.
package mail

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"fullstack"
	"fullstack/env"
)

// Message is one plain-text email.
type Message struct {
	To, Subject, Body string
}

// Mailer sends messages. Tests put their own into the container.
type Mailer interface {
	Send(ctx context.Context, m Message) error
}

// Config is the SMTP server (the stack's mail service).
type Config struct {
	Host     string `env:"MAIL_HOST"` // empty: emails are only logged (development)
	Port     int    `env:"MAIL_PORT" default:"587"`
	Username string `env:"MAIL_USERNAME"`
	Password string `env:"MAIL_PASSWORD"`
	From     string `env:"MAIL_FROM" default:"Luna <no-reply@luna.local>"`
}

// Provider registers a Mailer: SMTP when MAIL_HOST is set, otherwise one
// that writes the messages to the log (and refuses in production).
type Provider struct{}

// Register implements fullstack.Provider.
func (Provider) Register(app *fullstack.App) error {
	if fullstack.Has[Mailer](app) {
		return nil // set by a test
	}
	var cfg Config
	if err := env.Decode(&cfg); err != nil {
		return fmt.Errorf("mail: %w", err)
	}
	if cfg.Host == "" {
		fullstack.Set[Mailer](app, LogMailer{Log: app.Log, Production: !app.IsLocal() && !app.IsTesting()})
		return nil
	}
	fullstack.Set[Mailer](app, SMTPMailer{Config: cfg})
	return nil
}

// LogMailer writes messages to the log, for development.
type LogMailer struct {
	Log        *slog.Logger
	Production bool
}

// Send implements Mailer.
func (l LogMailer) Send(_ context.Context, m Message) error {
	if l.Production {
		return fmt.Errorf("mail: MAIL_HOST is not set; cannot send %q to %s", m.Subject, m.To)
	}
	l.Log.Info("mail (not sent: MAIL_HOST is empty)", "to", m.To, "subject", m.Subject, "body", m.Body)
	return nil
}

// SMTPMailer sends through an SMTP server with STARTTLS when it offers it.
type SMTPMailer struct{ Config Config }

// Send implements Mailer.
func (s SMTPMailer) Send(ctx context.Context, m Message) error {
	addr := net.JoinHostPort(s.Config.Host, strconv.Itoa(s.Config.Port))
	var auth smtp.Auth
	if s.Config.Username != "" {
		auth = smtp.PlainAuth("", s.Config.Username, s.Config.Password, s.Config.Host)
	}
	from := s.Config.From
	if i := strings.LastIndexByte(from, '<'); i >= 0 {
		from = strings.TrimSuffix(from[i+1:], ">")
	}
	msg := "From: " + s.Config.From + "\r\n" +
		"To: " + m.To + "\r\n" +
		"Subject: " + m.Subject + "\r\n" +
		"Date: " + time.Now().UTC().Format(time.RFC1123Z) + "\r\n" +
		"MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" +
		strings.ReplaceAll(m.Body, "\n", "\r\n")
	done := make(chan error, 1)
	go func() { done <- smtp.SendMail(addr, auth, from, []string{m.To}, []byte(msg)) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
