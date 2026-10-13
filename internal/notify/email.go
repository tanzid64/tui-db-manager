package notify

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"
)

type SMTPSenderFunc func(addr string, auth smtp.Auth, from string, to []string, msg []byte) error

type EmailOptions struct {
	Host     string
	Port     int
	From     string
	To       []string
	Username string
	Password string
	TLS      bool // true for direct SSL/TLS (port 465), false for STARTTLS (port 587)
	// Sender is an optional hook for testing; if nil, sends via real SMTP network.
	Sender SMTPSenderFunc
}

type EmailNotifier struct {
	host     string
	port     int
	from     string
	to       []string
	username string
	password string
	useTLS   bool
	sender   SMTPSenderFunc
}

func NewEmail(opts EmailOptions) (*EmailNotifier, error) {
	if opts.Host == "" {
		return nil, fmt.Errorf("email: SMTP host is required")
	}
	if opts.From == "" {
		return nil, fmt.Errorf("email: from address is required")
	}
	if len(opts.To) == 0 {
		return nil, fmt.Errorf("email: at least one recipient (to) is required")
	}
	port := opts.Port
	if port == 0 {
		if opts.TLS {
			port = 465
		} else {
			port = 587
		}
	}
	return &EmailNotifier{
		host:     opts.Host,
		port:     port,
		from:     opts.From,
		to:       opts.To,
		username: opts.Username,
		password: opts.Password,
		useTLS:   opts.TLS,
		sender:   opts.Sender,
	}, nil
}

func (e *EmailNotifier) Name() string { return "email" }

func (e *EmailNotifier) formatSubject(ev Event) string {
	switch ev.Type {
	case EventBackupSucceeded:
		return fmt.Sprintf("[dbk] Backup Succeeded: %s", ev.Job)
	case EventBackupFailed:
		return fmt.Sprintf("[dbk] Backup FAILED: %s", ev.Job)
	case EventRestoreDone:
		return fmt.Sprintf("[dbk] Restore Done: %s", ev.Job)
	case EventTest:
		return "[dbk] Test Notification"
	default:
		return fmt.Sprintf("[dbk] Notification: %s", ev.Job)
	}
}

func (e *EmailNotifier) formatBody(ev Event) string {
	var sb strings.Builder
	switch ev.Type {
	case EventBackupSucceeded:
		sb.WriteString(fmt.Sprintf("Backup for job %q succeeded.\n\n", ev.Job))
		sb.WriteString(fmt.Sprintf("Size: %d bytes\n", ev.Size))
		sb.WriteString(fmt.Sprintf("Started: %s\n", ev.Started.Format(time.RFC3339)))
		sb.WriteString(fmt.Sprintf("Elapsed: %s\n", ev.Elapsed.Round(time.Millisecond)))
	case EventBackupFailed:
		sb.WriteString(fmt.Sprintf("Backup for job %q FAILED.\n\n", ev.Job))
		sb.WriteString(fmt.Sprintf("Error: %s\n", ev.Err))
		sb.WriteString(fmt.Sprintf("Started: %s\n", ev.Started.Format(time.RFC3339)))
		sb.WriteString(fmt.Sprintf("Elapsed: %s\n", ev.Elapsed.Round(time.Millisecond)))
	case EventRestoreDone:
		sb.WriteString(fmt.Sprintf("Restore for job %q completed.\n\n", ev.Job))
		sb.WriteString(fmt.Sprintf("Elapsed: %s\n", ev.Elapsed.Round(time.Millisecond)))
	case EventTest:
		sb.WriteString("This is a test notification from dbk. SMTP connection and credentials are OK.\n")
	default:
		sb.WriteString(fmt.Sprintf("Job: %s\nMessage: %s\n", ev.Job, ev.Message))
	}
	return sb.String()
}

func (e *EmailNotifier) Send(ctx context.Context, ev Event) error {
	subject := e.formatSubject(ev)
	body := e.formatBody(ev)

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		e.from, strings.Join(e.to, ", "), subject, body)

	addr := fmt.Sprintf("%s:%d", e.host, e.port)

	var auth smtp.Auth
	if e.username != "" {
		auth = smtp.PlainAuth("", e.username, e.password, e.host)
	}

	if e.sender != nil {
		if err := e.sender(addr, auth, e.from, e.to, []byte(msg)); err != nil {
			return e.maskError(err)
		}
		return nil
	}

	// Real send implementation
	if e.useTLS {
		tlsConfig := &tls.Config{ServerName: e.host}
		conn, err := tls.Dial("tcp", addr, tlsConfig)
		if err != nil {
			return e.maskError(err)
		}
		client, err := smtp.NewClient(conn, e.host)
		if err != nil {
			conn.Close()
			return e.maskError(err)
		}
		defer client.Close()

		if auth != nil {
			if err := client.Auth(auth); err != nil {
				return e.maskError(err)
			}
		}
		if err := client.Mail(e.from); err != nil {
			return e.maskError(err)
		}
		for _, recipient := range e.to {
			if err := client.Rcpt(recipient); err != nil {
				return e.maskError(err)
			}
		}
		w, err := client.Data()
		if err != nil {
			return e.maskError(err)
		}
		if _, err := w.Write([]byte(msg)); err != nil {
			w.Close()
			return e.maskError(err)
		}
		if err := w.Close(); err != nil {
			return e.maskError(err)
		}
		return client.Quit()
	}

	// STARTTLS / standard port
	c, err := smtp.Dial(addr)
	if err != nil {
		return e.maskError(err)
	}
	defer c.Close()

	if ok, _ := c.Extension("STARTTLS"); ok {
		config := &tls.Config{ServerName: e.host}
		if err := c.StartTLS(config); err != nil {
			return e.maskError(err)
		}
	}

	if auth != nil {
		if ok, _ := c.Extension("AUTH"); ok {
			if err := c.Auth(auth); err != nil {
				return e.maskError(err)
			}
		}
	}

	if err := c.Mail(e.from); err != nil {
		return e.maskError(err)
	}
	for _, recipient := range e.to {
		if err := c.Rcpt(recipient); err != nil {
			return e.maskError(err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return e.maskError(err)
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		w.Close()
		return e.maskError(err)
	}
	if err := w.Close(); err != nil {
		return e.maskError(err)
	}
	return c.Quit()
}

func (e *EmailNotifier) Test(ctx context.Context) error {
	// For Test, if Sender hook is set, use it
	if e.sender != nil {
		return e.Send(ctx, Event{Type: EventTest, Job: "test"})
	}

	// For real network test: verify TCP dialability to SMTP server
	addr := fmt.Sprintf("%s:%d", e.host, e.port)
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return e.maskError(fmt.Errorf("connect to smtp server %s: %w", addr, err))
	}
	conn.Close()
	return nil
}

func (e *EmailNotifier) maskError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if e.password != "" {
		msg = strings.ReplaceAll(msg, e.password, "***")
	}
	return fmt.Errorf("%s", msg)
}
