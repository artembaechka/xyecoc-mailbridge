package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds process-wide settings (from environment).
type Config struct {
	// RemoteHTTPBase: origin для POST /request (обычно api, не CDN — иначе ответ unknown_service).
	RemoteHTTPBase string
	Lang           string

	// MailDefaultActionCurrentLang: значение JSON currentLang для mail/default (в вебе часто "inbox" даже для trash — см. Socket.IO).
	MailDefaultActionCurrentLang string

	// MailViewCurrentLang: currentLang для POST mail/view (веб шлёт "mail").
	MailViewCurrentLang string

	// MailComposeCurrentLang: currentLang для mail/message-new и mail/reply-confirm (веб шлёт "mail", не ru).
	MailComposeCurrentLang string

	// RemoteAssetBase: origin для GET /mail/... и вложений; пусто → для api.xyecoc.com подставляется cdn.xyecoc.com, иначе как RemoteHTTPBase.
	RemoteAssetBase string

	// DefaultMailDomain: if SMTP/IMAP user has no "@", append "@DefaultMailDomain" (пусто = не дописывать).
	DefaultMailDomain string

	// RemoteAuthEmailLocalPartOnly: в account/authorization в поле email слать только локальную часть (baechka), не full address.
	RemoteAuthEmailLocalPartOnly bool

	IMAPAddr string
	SMTPAddr string

	// IMAPAllowInsecureAuth: разрешить LOGIN/PLAIN без TLS (нужно за reverse proxy, где TLS снимается до контейнера).
	IMAPAllowInsecureAuth bool

	HTTPTimeout time.Duration
	MaxMailList int // safety cap when paginating remote default action

	TrustProxyHeader bool // reserved: X-Forwarded-For handling if extended

	// RemoteHTTPTrace: логировать тело POST /request и ответы (с маскированием секретов). По умолчанию включено при LOG_LEVEL=debug.
	RemoteHTTPTrace bool

	// ProtocolLog: построчный лог IMAP/SMTP (команды и ответы; пароль в IMAP LOGIN маскируется).
	ProtocolLog bool

	// TagDefaultColor: hex без # для mail/tag-new (CREATE tag/… или Labels/…).
	TagDefaultColor string
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getenvBool(key string, def bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if v == "" {
		return def
	}
	return v == "1" || v == "true" || v == "yes"
}

// defaultMailDomain: пустой env → не дописывать домен; можно задать xyecoc.com явно.
func defaultMailDomain() string {
	v := strings.TrimSpace(os.Getenv("MAIL_DEFAULT_DOMAIN"))
	if strings.EqualFold(v, "off") || v == "-" {
		return ""
	}
	return v
}

func Load() Config {
	timeout := 60 * time.Second
	if v := os.Getenv("HTTP_TIMEOUT_SEC"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			timeout = time.Duration(n) * time.Second
		}
	}
	maxList := 5000
	if v := os.Getenv("MAX_MAIL_LIST"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxList = n
		}
	}
	defHTTPTrace := strings.EqualFold(strings.TrimSpace(os.Getenv("LOG_LEVEL")), "debug")
	httpBase := getenv("REMOTE_HTTP_BASE", "https://api.xyecoc.com")
	assetBase := strings.TrimSpace(os.Getenv("REMOTE_ASSET_BASE"))
	if assetBase == "" {
		// GET /mail/... и /data/attachments/... на проде лежат на CDN; POST /request — только api.
		if strings.Contains(strings.ToLower(httpBase), "api.xyecoc.com") {
			assetBase = "https://cdn.xyecoc.com"
		}
	}
	return Config{
		RemoteHTTPBase: httpBase,
		RemoteAssetBase: assetBase,
		Lang:           getenv("MAILBRIDGE_LANG", "ru"),
		MailDefaultActionCurrentLang: getenv("MAILBRIDGE_MAIL_CURRENT_LANG", "inbox"),
		MailViewCurrentLang:          getenv("MAILBRIDGE_MAIL_VIEW_LANG", "mail"),
		MailComposeCurrentLang:       getenv("MAILBRIDGE_MAIL_COMPOSE_LANG", "mail"),
		DefaultMailDomain: defaultMailDomain(),
		RemoteAuthEmailLocalPartOnly: getenvBool("REMOTE_AUTH_EMAIL_LOCAL_PART_ONLY", true),
		IMAPAddr:       getenv("IMAP_ADDR", ":143"),
		SMTPAddr:       getenv("SMTP_ADDR", ":587"),
		IMAPAllowInsecureAuth: getenvBool("IMAP_ALLOW_INSECURE_AUTH", true),
		HTTPTimeout:    timeout,
		MaxMailList:    maxList,
		TrustProxyHeader: getenv("TRUST_PROXY", "") == "1",
		RemoteHTTPTrace:  getenvBool("REMOTE_HTTP_TRACE", defHTTPTrace),
		ProtocolLog:      getenvBool("MAILBRIDGE_PROTOCOL_LOG", false),
		TagDefaultColor: strings.TrimPrefix(strings.TrimSpace(getenv("MAILBRIDGE_TAG_DEFAULT_COLOR", "ADACF1")), "#"),
	}
}
