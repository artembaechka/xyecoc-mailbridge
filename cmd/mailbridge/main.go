package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/emersion/go-imap/server"

	"github.com/artembaechka/mailbridge/internal/config"
	"github.com/artembaechka/mailbridge/internal/imapbackend"
	"github.com/artembaechka/mailbridge/internal/logx"
	"github.com/artembaechka/mailbridge/internal/loginlimit"
	"github.com/artembaechka/mailbridge/internal/protocollog"
	"github.com/artembaechka/mailbridge/internal/smtpbridge"
)

func main() {
	log := logx.New()
	cfg := config.Load()

	log.Info("mailbridge starting",
		"remote_http_base", cfg.RemoteHTTPBase,
		"remote_asset_base", cfg.RemoteAssetBase,
		"lang", cfg.Lang,
		"mail_default_action_current_lang", cfg.MailDefaultActionCurrentLang,
		"mail_view_current_lang", cfg.MailViewCurrentLang,
		"mail_compose_current_lang", cfg.MailComposeCurrentLang,
		"mail_default_domain", cfg.DefaultMailDomain,
		"remote_auth_email_local_part_only", cfg.RemoteAuthEmailLocalPartOnly,
		"remote_http_trace", cfg.RemoteHTTPTrace,
		"imap", cfg.IMAPAddr,
		"imap_allow_insecure_auth", cfg.IMAPAllowInsecureAuth,
		"smtp", cfg.SMTPAddr,
		"protocol_log", cfg.ProtocolLog,
	)

	loginLim := loginlimit.New()
	be := imapbackend.New(cfg, log, loginLim)
	s := server.New(be)
	s.Addr = cfg.IMAPAddr
	s.AllowInsecureAuth = cfg.IMAPAllowInsecureAuth
	s.ErrorLog = slog.NewLogLogger(log.Handler(), slog.LevelWarn)
	if cfg.ProtocolLog {
		s.Debug = protocollog.IMAPServerDebug(log)
	}

	smtpSrv := smtpbridge.New(cfg, log, loginLim)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		log.Info("IMAP listening", "addr", cfg.IMAPAddr)
		if err := s.ListenAndServe(); err != nil {
			log.Error("IMAP exit", "err", err)
		}
	}()
	go func() {
		defer wg.Done()
		log.Info("SMTP listening", "addr", cfg.SMTPAddr)
		if err := smtpSrv.ListenTCP(); err != nil {
			log.Error("SMTP exit", "err", err)
		}
	}()

	<-ctx.Done()
	stop()
	log.Info("shutting down")
	if err := s.Close(); err != nil {
		log.Warn("IMAP server close", "err", err.Error())
	}
	smtpSrv.Stop()
	wg.Wait()
	os.Exit(0)
}
