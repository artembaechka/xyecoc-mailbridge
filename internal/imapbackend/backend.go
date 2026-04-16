package imapbackend

import (
	"context"
	"log/slog"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/backend"

	"github.com/artembaechka/mailbridge/internal/config"
	"github.com/artembaechka/mailbridge/internal/identity"
	"github.com/artembaechka/mailbridge/internal/loginlimit"
	"github.com/artembaechka/mailbridge/internal/remote"
)

// Backend implements go-imap/backend.Backend (remote xyecoc API).
type Backend struct {
	Cfg    config.Config
	Log    *slog.Logger
	Limit  *loginlimit.Tracker
}

func New(cfg config.Config, log *slog.Logger, limit *loginlimit.Tracker) *Backend {
	if log == nil {
		log = slog.Default()
	}
	return &Backend{Cfg: cfg, Log: log, Limit: limit}
}

// Login validates credentials against account/authorization and returns a remote-backed User.
func (b *Backend) Login(connInfo *imap.ConnInfo, username, password string) (backend.User, error) {
	ip := ""
	if connInfo != nil {
		ip = loginlimit.ClientIP(connInfo.RemoteAddr)
	}
	if b.Limit != nil && !b.Limit.Allowed(ip) {
		b.Log.Warn("IMAP login rate limited", "ip", ip)
		return nil, backend.ErrInvalidCredentials
	}

	login := identity.ExpandLocalPart(username, b.Cfg.DefaultMailDomain)
	apiEmail := identity.RemoteAuthEmail(login, b.Cfg.RemoteAuthEmailLocalPartOnly)
	cli := remote.NewClient(b.Cfg.RemoteHTTPBase, b.Cfg.Lang, b.Cfg.HTTPTimeout)
	cli.AssetURL = b.Cfg.RemoteAssetBase
	cli.Log = b.Log
	cli.Trace = b.Cfg.RemoteHTTPTrace
	if err := cli.Authorize(context.Background(), apiEmail, password); err != nil {
		if b.Limit != nil {
			b.Limit.RecordFailure(ip)
		}
		b.Log.Info("IMAP login failed", "login", login, "remote_email", apiEmail, "err", err.Error())
		return nil, backend.ErrInvalidCredentials
	}
	if b.Limit != nil {
		b.Limit.RecordSuccess(ip)
	}
	b.Log.Info("IMAP login ok", "login", login, "remote_email", apiEmail)
	return newUser(login, cli, b.Cfg.MaxMailList, b.Cfg.MailDefaultActionCurrentLang, b.Cfg.MailViewCurrentLang, b.Cfg.MailComposeCurrentLang, b.Cfg.TagDefaultColor, b.Log), nil
}
