package smtpbridge

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	mhale "github.com/artembaechka/mailbridge/internal/mhalesmtpd"

	"github.com/artembaechka/mailbridge/internal/config"
	"github.com/artembaechka/mailbridge/internal/identity"
	"github.com/artembaechka/mailbridge/internal/loginlimit"
	"github.com/artembaechka/mailbridge/internal/protocollog"
	"github.com/artembaechka/mailbridge/internal/remote"
)

// Server wraps mhale/smtpd for submission via mail/message-new or mail/reply-confirm.
// Auth stores one *remote.Client per remote address (last AUTH wins for that IP).
type Server struct {
	cfg    config.Config
	log    *slog.Logger
	limit  *loginlimit.Tracker
	mu     sync.Mutex
	tokens map[string]*remote.Client

	smtpMu   sync.Mutex // protects mhaleSrv
	mhaleSrv *mhale.Server
}

func New(cfg config.Config, log *slog.Logger, limit *loginlimit.Tracker) *Server {
	if log == nil {
		log = slog.Default()
	}
	return &Server{cfg: cfg, log: log, limit: limit, tokens: make(map[string]*remote.Client)}
}

func (s *Server) authHandler(remoteAddr net.Addr, _ string, username []byte, password []byte, _ []byte) (bool, error) {
	ip := loginlimit.ClientIP(remoteAddr)
	if s.limit != nil && !s.limit.Allowed(ip) {
		s.log.Warn("SMTP auth rate limited", "ip", ip, "addr", remoteAddr.String())
		return false, nil
	}
	raw := strings.TrimSpace(string(username))
	login := identity.ExpandLocalPart(raw, s.cfg.DefaultMailDomain)
	apiEmail := identity.RemoteAuthEmail(login, s.cfg.RemoteAuthEmailLocalPartOnly)
	pass := string(password)
	cli := remote.NewClient(s.cfg.RemoteHTTPBase, s.cfg.Lang, s.cfg.HTTPTimeout)
	cli.Log = s.log
	cli.Trace = s.cfg.RemoteHTTPTrace
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := cli.Authorize(ctx, apiEmail, pass); err != nil {
		if s.limit != nil {
			s.limit.RecordFailure(ip)
		}
		s.log.Info("SMTP auth failed", "addr", remoteAddr.String(), "login", login, "remote_email", apiEmail, "err", err.Error())
		return false, nil
	}
	if s.limit != nil {
		s.limit.RecordSuccess(ip)
	}
	s.mu.Lock()
	s.tokens[remoteAddr.String()] = cli
	s.mu.Unlock()
	s.log.Info("SMTP auth ok", "login", login, "remote_email", apiEmail)
	return true, nil
}

func (s *Server) mailHandler(remoteAddr net.Addr, _ string, to []string, data []byte) error {
	s.mu.Lock()
	cli := s.tokens[remoteAddr.String()]
	s.mu.Unlock()
	if cli == nil {
		return fmt.Errorf("530 5.7.0 Authentication required")
	}
	subj, html, replyMailID, attaches, err := parseOutboundMail(data)
	if err != nil {
		return fmt.Errorf("451 4.3.0 bad message data")
	}
	if attaches == nil {
		attaches = []any{}
	}
	recipients := normalizeSMTPRecipients(to, s.cfg.DefaultMailDomain)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var resp *remote.Response
	if replyMailID != "" {
		resp, err = cli.DoReplyConfirm(ctx, replyMailID, html, attaches, s.cfg.MailComposeCurrentLang)
	} else {
		resp, err = cli.Do(ctx, &remote.Request{
			Service: "mail",
			Action:  "message-new",
			Data: map[string]any{
				"subject":  subj,
				"message":  html,
				"users":    recipients,
				"attaches": attaches,
			},
			CurrentLang: s.cfg.MailComposeCurrentLang,
			Token:       cli.Token(),
		})
	}
	if err != nil {
		s.log.Error("SMTP send", "err", err)
		return fmt.Errorf("451 4.3.0 upstream error")
	}
	if !remote.MailMutationOK(resp) {
		action := "message-new"
		if replyMailID != "" {
			action = "reply-confirm"
		}
		s.log.Info(action+" rejected by API",
			"remote_status", resp.Status,
			"remote_message", resp.Message,
			"to", recipients,
			"reply_mail_id", replyMailID,
		)
		if strings.Contains(strings.ToLower(resp.Message), "denied") {
			return fmt.Errorf("554 5.7.1 policy rejection")
		}
		return fmt.Errorf("554 5.5.0 send failed")
	}
	if replyMailID != "" {
		s.log.Info("SMTP reply sent", "mail_id", replyMailID)
	} else {
		s.log.Info("SMTP message sent", "to", recipients)
	}
	return nil
}

// normalizeSMTPRecipients дописывает MAIL_DEFAULT_DOMAIN к RCPT без «@» (как в web-клиенте с полным email).
func normalizeSMTPRecipients(rcpt []string, defaultDomain string) string {
	var parts []string
	for _, a := range rcpt {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		parts = append(parts, identity.ExpandLocalPart(a, defaultDomain))
	}
	return strings.Join(parts, ",")
}

func parseMailFromSMTPData(data []byte) (subject, html string, err error) {
	s := string(data)
	parts := strings.SplitN(s, "\r\n\r\n", 2)
	if len(parts) < 2 {
		parts = strings.SplitN(s, "\n\n", 2)
	}
	hdr := parts[0]
	body := ""
	if len(parts) == 2 {
		body = strings.TrimSpace(parts[1])
	}
	for _, ln := range strings.Split(hdr, "\n") {
		if strings.HasPrefix(strings.ToLower(ln), "subject:") {
			if i := strings.IndexByte(ln, ':'); i >= 0 {
				subject = strings.TrimSpace(ln[i+1:])
			}
		}
	}
	if subject == "" {
		subject = "(no subject)"
	}
	if body == "" {
		body = "<html><body></body></html>"
	}
	return subject, body, nil
}

// tcpNoDelayListener enables TCP_NODELAY on each accepted connection. Without it, the
// small final reply after DATA (e.g. "250 2.0.0 Ok: queued") can sit in the Nagle
// buffer while the client waits — some MUAs then report a timeout or missing confirmation.
type tcpNoDelayListener struct {
	net.Listener
}

func (l tcpNoDelayListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	if tc, ok := c.(*net.TCPConn); ok {
		_ = tc.SetNoDelay(true)
		_ = tc.SetKeepAlive(true)
	}
	return c, nil
}

// Stop signals the SMTP server to shut down (unblocks Serve).
func (s *Server) Stop() {
	s.smtpMu.Lock()
	srv := s.mhaleSrv
	s.smtpMu.Unlock()
	if srv != nil {
		_ = srv.Close()
	}
}

// ListenTCP starts the SMTP submission listener.
func (s *Server) ListenTCP() error {
	srv := &mhale.Server{
		Addr:         s.cfg.SMTPAddr,
		Appname:      "mailbridge",
		Hostname:     "mailbridge",
		AuthHandler:  s.authHandler,
		AuthRequired: true,
		Handler:      s.mailHandler,
		Timeout:      5 * time.Minute,
		MaxSize:      25 * 1024 * 1024,
		// mhale/smtpd по умолчанию объявляет PLAIN/LOGIN только после TLS; без STARTTLS клиент видит
		// в основном CRAM-MD5 — типичные клиенты тогда пишут «метод не поддерживается».
		AuthMechs: map[string]bool{
			"PLAIN":    true,
			"LOGIN":    true,
			"CRAM-MD5": false,
		},
	}
	s.smtpMu.Lock()
	s.mhaleSrv = srv
	s.smtpMu.Unlock()
	defer func() {
		s.smtpMu.Lock()
		s.mhaleSrv = nil
		s.smtpMu.Unlock()
	}()

	if s.cfg.ProtocolLog {
		mhale.Debug = true
		srv.LogRead = func(remoteIP, verb, line string) {
			s.log.Info("smtp wire", "remote_ip", remoteIP, "dir", "read", "line", protocollog.RedactSMTP(line))
		}
		srv.LogWrite = func(remoteIP, verb, line string) {
			s.log.Info("smtp wire", "remote_ip", remoteIP, "dir", "write", "line", line)
		}
	}
	addr := s.cfg.SMTPAddr
	if addr == "" {
		addr = ":25"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return srv.Serve(tcpNoDelayListener{Listener: ln})
}
