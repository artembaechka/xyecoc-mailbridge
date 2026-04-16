package imapbackend

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap/backend"

	"github.com/artembaechka/mailbridge/internal/remote"
)

type User struct {
	email   string
	cli     *remote.Client
	maxList int
	// mailDefaultActionCurrentLang: поле currentLang для POST mail/default (веб шлёт "inbox", не ru).
	mailDefaultActionCurrentLang string
	// mailViewCurrentLang: currentLang для POST mail/view (веб шлёт "mail").
	mailViewCurrentLang string
	// mailComposeCurrentLang: mail/message-new (черновик), как SMTP / веб compose.
	mailComposeCurrentLang string
	// tagDefaultColor: mail/tag-new (hex без #).
	tagDefaultColor string
	log *slog.Logger

	cacheMu sync.Mutex
	// key: specCacheKey(MailboxSpec) -> cached summaries
	listCache map[string]*summaryCache

	nameMu         sync.RWMutex
	specByListName map[string]MailboxSpec // LIST names + Folder-<id>, Tag-<name>, Tag-<id> -> spec
}

type summaryCache struct {
	at        time.Time
	summaries []remote.MailSummary
	spec      MailboxSpec
}

func newUser(email string, cli *remote.Client, maxList int, mailDefaultActionCurrentLang, mailViewCurrentLang, mailComposeCurrentLang, tagDefaultColor string, log *slog.Logger) *User {
	if mailDefaultActionCurrentLang == "" {
		mailDefaultActionCurrentLang = "inbox"
	}
	if mailViewCurrentLang == "" {
		mailViewCurrentLang = "mail"
	}
	if mailComposeCurrentLang == "" {
		mailComposeCurrentLang = "mail"
	}
	if tagDefaultColor == "" {
		tagDefaultColor = "ADACF1"
	}
	tagDefaultColor = strings.TrimPrefix(strings.TrimSpace(tagDefaultColor), "#")
	return &User{
		email:                        email,
		cli:                          cli,
		maxList:                      maxList,
		mailDefaultActionCurrentLang: mailDefaultActionCurrentLang,
		mailViewCurrentLang:          mailViewCurrentLang,
		mailComposeCurrentLang:       mailComposeCurrentLang,
		tagDefaultColor:              tagDefaultColor,
		log:                          log,
		listCache:                    make(map[string]*summaryCache),
	}
}

func (u *User) Username() string { return u.email }

func (u *User) invalidateSpec(spec MailboxSpec) {
	u.cacheMu.Lock()
	delete(u.listCache, specCacheKey(spec))
	u.cacheMu.Unlock()
}

func (u *User) invalidateFolderTagListCaches() {
	u.cacheMu.Lock()
	defer u.cacheMu.Unlock()
	for k := range u.listCache {
		if strings.HasPrefix(k, "folders/") || strings.HasPrefix(k, "tags/") {
			delete(u.listCache, k)
		}
	}
}

func (u *User) ListMailboxes(_ bool) ([]backend.Mailbox, error) {
	ctx := context.Background()
	var out []backend.Mailbox

	std := []string{"INBOX", "Sent", "Drafts", "Junk", "Trash", "Important"}
	for _, n := range std {
		spec, err := ParseMailboxName(n)
		if err != nil {
			continue
		}
		out = append(out, newMailbox(u, CanonicalName(spec), spec))
	}

	resp, err := u.cli.Do(ctx, &remote.Request{
		Service: "account",
		Action:  "folders_tags",
		Token:   u.cli.Token(),
	})
	if err != nil {
		u.log.Debug("folders_tags optional failed", "err", err)
		_, _ = u.rebuildMailboxNameIndex(nil)
		return out, nil
	}
	if resp.Status != 1 {
		_, _ = u.rebuildMailboxNameIndex(nil)
		return out, nil
	}
	extras, _ := u.rebuildMailboxNameIndex(resp)
	for _, e := range extras {
		out = append(out, newMailbox(u, e.Name, e.Spec))
	}
	return out, nil
}

// rebuildMailboxNameIndex resets specByListName and returns folder/tag mailboxes in API order for LIST.
func (u *User) rebuildMailboxNameIndex(resp *remote.Response) (extras []namedFolderTagMailbox, _ error) {
	u.nameMu.Lock()
	defer u.nameMu.Unlock()
	u.specByListName = make(map[string]MailboxSpec)
	std := []string{"INBOX", "Sent", "Drafts", "Junk", "Trash", "Important"}
	for _, n := range std {
		spec, err := ParseMailboxName(n)
		if err != nil {
			continue
		}
		u.specByListName[n] = spec
		u.specByListName[CanonicalName(spec)] = spec
	}
	if resp == nil {
		return nil, nil
	}
	occupied := make(map[string]struct{})
	for k := range u.specByListName {
		occupied[k] = struct{}{}
	}
	for _, f := range resp.Folders {
		spec := MailboxSpec{Param1: "folders", Param2: strconv.Itoa(f.ID)}
		disp := uniqueDisplayName(f.Name, spec, occupied)
		u.specByListName[disp] = spec
		u.specByListName[CanonicalName(spec)] = spec
		extras = append(extras, namedFolderTagMailbox{Name: disp, Spec: spec})
	}
	for _, t := range resp.Tags {
		spec := MailboxSpec{Param1: "tags", Param2: strconv.Itoa(t.ID)}
		disp := uniqueTagDisplayName(t.Name, spec, occupied)
		u.specByListName[disp] = spec
		u.specByListName[CanonicalName(spec)] = spec
		extras = append(extras, namedFolderTagMailbox{Name: disp, Spec: spec})
	}
	return extras, nil
}

type namedFolderTagMailbox struct {
	Name string
	Spec MailboxSpec
}

// refreshFolderTagNameIndex reloads folders_tags into specByListName (e.g. before Resolve).
func (u *User) refreshFolderTagNameIndex() {
	ctx := context.Background()
	resp, err := u.cli.Do(ctx, &remote.Request{
		Service: "account",
		Action:  "folders_tags",
		Token:   u.cli.Token(),
	})
	if err != nil || resp.Status != 1 {
		return
	}
	_, _ = u.rebuildMailboxNameIndex(resp)
}

// resolveFromIndex maps a LIST/SELECT name via folders_tags cache (display names, Tag-<name>).
func (u *User) resolveFromIndex(name string) (MailboxSpec, bool) {
	u.nameMu.RLock()
	spec, ok := u.specByListName[name]
	u.nameMu.RUnlock()
	if ok {
		return spec, true
	}
	u.refreshFolderTagNameIndex()
	u.nameMu.RLock()
	spec, ok = u.specByListName[name]
	if !ok {
		for k, v := range u.specByListName {
			if strings.EqualFold(k, name) {
				spec = v
				ok = true
				break
			}
		}
	}
	u.nameMu.RUnlock()
	return spec, ok
}

// resolveMailboxSpecOnce maps one candidate string (flat LIST name or leaf segment).
func (u *User) resolveMailboxSpecOnce(candidate string) (MailboxSpec, error) {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return MailboxSpec{}, fmt.Errorf("empty mailbox name")
	}
	// Names starting with "tag-" must hit the index first so Tag-<label> is not
	// mistaken for Tag-<id> when the label is numeric but differs from the id.
	tryTagFirst := strings.HasPrefix(strings.ToLower(candidate), "tag-")
	if tryTagFirst {
		if spec, ok := u.resolveFromIndex(candidate); ok {
			return spec, nil
		}
	}
	if spec, err := ParseMailboxName(candidate); err == nil {
		return spec, nil
	}
	if !tryTagFirst {
		if spec, ok := u.resolveFromIndex(candidate); ok {
			return spec, nil
		}
	}
	return MailboxSpec{}, fmt.Errorf("unknown mailbox")
}

// ResolveMailboxSpec maps IMAP mailbox name (LIST/SELECT) to remote params.
func (u *User) ResolveMailboxSpec(name string) (MailboxSpec, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return MailboxSpec{}, fmt.Errorf("empty mailbox name")
	}
	leaf := imapMailboxLeaf(name)
	seen := make(map[string]struct{})
	for _, cand := range []string{name, leaf} {
		cand = strings.TrimSpace(cand)
		if cand == "" {
			continue
		}
		if _, ok := seen[cand]; ok {
			continue
		}
		seen[cand] = struct{}{}
		if spec, err := u.resolveMailboxSpecOnce(cand); err == nil {
			return spec, nil
		}
	}
	return MailboxSpec{}, fmt.Errorf("unknown mailbox")
}

func (u *User) GetMailbox(name string) (backend.Mailbox, error) {
	spec, err := u.ResolveMailboxSpec(name)
	if err != nil {
		return nil, backend.ErrNoSuchMailbox
	}
	return newMailbox(u, name, spec), nil
}

func (u *User) CreateMailbox(name string) error {
	var apiName string
	tagCreate := false
	if label, ok := tagCreateDisplayName(name); ok {
		tagCreate = true
		apiName = label
	} else {
		var err error
		apiName, err = folderNameForAPI(name)
		if err != nil {
			return err
		}
	}

	u.refreshFolderTagNameIndex()
	if spec, err := u.ResolveMailboxSpec(apiName); err == nil {
		if spec.Param1 == "folders" || spec.Param1 == "tags" {
			return backend.ErrMailboxAlreadyExists
		}
	}

	ctx := context.Background()
	var resp *remote.Response
	var err error
	if tagCreate {
		resp, err = u.cli.Do(ctx, &remote.Request{
			Service:     "mail",
			Action:      "tag-new",
			CurrentLang: u.mailViewCurrentLang,
			Data: map[string]any{
				"name":  apiName,
				"color": u.tagDefaultColor,
			},
			Token: u.cli.Token(),
		})
	} else {
		resp, err = u.cli.Do(ctx, &remote.Request{
			Service:     "mail",
			Action:      "folder-new",
			CurrentLang: u.mailViewCurrentLang,
			Data: map[string]any{
				"name": apiName,
			},
			Token: u.cli.Token(),
		})
	}
	if err != nil {
		return err
	}
	if !remote.MailMutationOK(resp) {
		msg := strings.TrimSpace(remote.ResponseErrMsg(resp))
		if msg == "" {
			msg = strings.TrimSpace(resp.Message)
		}
		if folderNewLooksDuplicate(msg) {
			return backend.ErrMailboxAlreadyExists
		}
		if tagCreate {
			return fmt.Errorf("tag-new: %s", msg)
		}
		return fmt.Errorf("folder-new: %s", msg)
	}
	u.invalidateFolderTagListCaches()
	u.refreshFolderTagNameIndex()
	return nil
}

func folderNewLooksDuplicate(msg string) bool {
	m := strings.ToLower(msg)
	return strings.Contains(m, "exist") ||
		strings.Contains(m, "duplicate") ||
		strings.Contains(m, "already") ||
		strings.Contains(m, "same") ||
		strings.Contains(m, "уже")
}

// remoteAccountLang is currentLang for account-style mail mutations (same as browser account context).
func (u *User) remoteAccountLang() string {
	s := strings.TrimSpace(u.cli.Lang)
	if s == "" {
		return "ru"
	}
	return s
}

func (u *User) folderByID(ctx context.Context, id int) (remote.Folder, bool) {
	resp, err := u.cli.Do(ctx, &remote.Request{
		Service: "account",
		Action:  "folders_tags",
		Token:   u.cli.Token(),
	})
	if err != nil || resp.Status != 1 {
		return remote.Folder{}, false
	}
	for _, f := range resp.Folders {
		if f.ID == id {
			return f, true
		}
	}
	return remote.Folder{}, false
}

func (u *User) DeleteMailbox(name string) error {
	spec, err := u.ResolveMailboxSpec(name)
	if err != nil {
		return backend.ErrNoSuchMailbox
	}
	if spec.Param2 == "" || (spec.Param1 != "folders" && spec.Param1 != "tags") {
		return fmt.Errorf("only user folders and tags can be deleted")
	}
	id, err := strconv.Atoi(spec.Param2)
	if err != nil || id <= 0 {
		return backend.ErrNoSuchMailbox
	}
	ctx := context.Background()
	action := "folder-delete"
	data := map[string]any{"id": id, "folder_id": id}
	lang := u.remoteAccountLang()
	if spec.Param1 == "tags" {
		action = "tag-delete"
		data = map[string]any{"id": id}
		// Same mail UI context as tag-new / mail-action; folder-delete uses account lang.
		lang = u.mailViewCurrentLang
	}
	resp, err := u.cli.Do(ctx, &remote.Request{
		Service:     "mail",
		Action:      action,
		CurrentLang: lang,
		Data:        data,
		Token:       u.cli.Token(),
	})
	if err != nil {
		return err
	}
	if !remote.MailMutationOK(resp) {
		msg := strings.TrimSpace(remote.ResponseErrMsg(resp))
		if msg == "" {
			msg = strings.TrimSpace(resp.Message)
		}
		return fmt.Errorf("%s: %s", action, msg)
	}
	u.invalidateSpec(spec)
	u.invalidateFolderTagListCaches()
	u.refreshFolderTagNameIndex()
	return nil
}

func (u *User) RenameMailbox(existing, newName string) error {
	spec, err := u.ResolveMailboxSpec(existing)
	if err != nil {
		return backend.ErrNoSuchMailbox
	}
	if spec.Param1 != "folders" || spec.Param2 == "" {
		return fmt.Errorf("rename supported for folders only")
	}
	id, err := strconv.Atoi(spec.Param2)
	if err != nil || id <= 0 {
		return backend.ErrNoSuchMailbox
	}
	apiName, err := folderNameForAPI(newName)
	if err != nil {
		return err
	}
	ctx := context.Background()
	f, ok := u.folderByID(ctx, id)
	if !ok {
		return backend.ErrNoSuchMailbox
	}
	if strings.TrimSpace(f.Name) == strings.TrimSpace(apiName) {
		return nil
	}
	resp, err := u.cli.Do(ctx, &remote.Request{
		Service:     "mail",
		Action:      "folder-edit",
		CurrentLang: u.mailViewCurrentLang,
		Data: map[string]any{
			"id":       id,
			"name":     apiName,
			"from":     f.From,
			"contains": f.Contains,
		},
		Token: u.cli.Token(),
	})
	if err != nil {
		return err
	}
	if !remote.MailMutationOK(resp) {
		msg := strings.TrimSpace(remote.ResponseErrMsg(resp))
		if msg == "" {
			msg = strings.TrimSpace(resp.Message)
		}
		return fmt.Errorf("folder-edit: %s", msg)
	}
	u.invalidateSpec(spec)
	u.invalidateFolderTagListCaches()
	u.refreshFolderTagNameIndex()
	return nil
}

func (u *User) Logout() error {
	return nil
}
