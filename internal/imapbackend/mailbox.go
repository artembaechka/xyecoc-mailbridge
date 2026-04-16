package imapbackend

import (
	"context"
	"fmt"
	"hash/fnv"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap"

	"github.com/artembaechka/mailbridge/internal/remote"
)

const summaryCacheTTL = 45 * time.Second

// Mailbox implements backend.Mailbox.
type Mailbox struct {
	user *User
	name string // canonical
	spec MailboxSpec

	uidValidity uint32

	flagMu sync.Mutex
	// uids with \Deleted pending expunge
	pendingDeleted map[uint32]struct{}
}

func newMailbox(user *User, name string, spec MailboxSpec) *Mailbox {
	h := fnv.New32a()
	_, _ = h.Write([]byte(user.email + "|" + name + "|v1"))
	return &Mailbox{
		user:           user,
		name:           name,
		spec:           spec,
		uidValidity:    h.Sum32(),
		pendingDeleted: make(map[uint32]struct{}),
	}
}

func (m *Mailbox) Name() string { return m.name }

func (m *Mailbox) Info() (*imap.MailboxInfo, error) {
	return &imap.MailboxInfo{
		Delimiter: "/",
		Name:      m.name,
	}, nil
}

func (m *Mailbox) Status(items []imap.StatusItem) (*imap.MailboxStatus, error) {
	sums, err := m.user.loadSummaries(context.Background(), m.spec)
	if err != nil {
		return nil, err
	}
	st := imap.NewMailboxStatus(m.name, items)
	st.Flags = []string{imap.SeenFlag, imap.DeletedFlag, imap.FlaggedFlag, imap.DraftFlag, imap.AnsweredFlag}
	st.PermanentFlags = append([]string(nil), st.Flags...)
	st.PermanentFlags = append(st.PermanentFlags, "\\*")

	for _, item := range items {
		switch item {
		case imap.StatusMessages:
			st.Messages = uint32(len(sums))
		case imap.StatusUidNext:
			st.UidNext = m.nextUID(sums)
		case imap.StatusUidValidity:
			st.UidValidity = m.uidValidity
		case imap.StatusUnseen:
			st.Unseen = m.countUnseen(sums)
		case imap.StatusRecent:
			st.Recent = 0
		}
	}
	return st, nil
}

func (m *Mailbox) nextUID(sums []remote.MailSummary) uint32 {
	var max uint32
	for _, s := range sums {
		u := uint32(s.ID)
		if u > max {
			max = u
		}
	}
	return max + 1
}

func (m *Mailbox) countUnseen(sums []remote.MailSummary) uint32 {
	var n uint32
	for _, s := range sums {
		if !s.Read {
			n++
		}
	}
	return n
}

func (m *Mailbox) SetSubscribed(bool) error { return nil }
func (m *Mailbox) Check() error             { return nil }

func (m *Mailbox) ListMessages(uid bool, seqSet *imap.SeqSet, items []imap.FetchItem, ch chan<- *imap.Message) error {
	defer close(ch)
	sums, err := m.user.loadSummaries(context.Background(), m.spec)
	if err != nil {
		return err
	}
	sort.Slice(sums, func(i, j int) bool { return sums[i].ID < sums[j].ID })

	maxSeq, maxUID := mailboxMaxSeqAndUID(sums)

	for i := range sums {
		seqNum := uint32(i + 1)
		id := uint32(sums[i].ID)
		var matchID uint32
		if uid {
			matchID = id
		} else {
			matchID = seqNum
		}
		if !seqSetAddressesMessage(uid, seqSet, matchID, maxSeq, maxUID) {
			continue
		}

		var lit *msgLiteral
		var err error
		if needsFullRFC822(items) {
			lit, err = m.buildLiteral(context.Background(), &sums[i])
			if err != nil {
				m.user.log.Warn("mail view failed, using list preview", "id", sums[i].ID, "err", err.Error())
				lit = m.buildLiteralPreview(&sums[i])
			}
		} else {
			lit = m.buildLiteralPreview(&sums[i])
		}
		msg, err := lit.Fetch(seqNum, items)
		if err != nil {
			m.user.log.Warn("imap fetch item build failed", "id", sums[i].ID, "err", err.Error())
			continue
		}
		ch <- msg
	}
	return nil
}

func needsFullRFC822(items []imap.FetchItem) bool {
	for _, it := range items {
		switch it {
		case imap.FetchRFC822, imap.FetchRFC822Header, imap.FetchRFC822Text,
			imap.FetchBody, imap.FetchBodyStructure:
			return true
		}
		if _, err := imap.ParseBodySectionName(it); err == nil {
			return true
		}
	}
	return false
}

func (m *Mailbox) buildLiteralPreview(s *remote.MailSummary) *msgLiteral {
	uid := uint32(s.ID)
	t := parseTime(s.CreatedAt)
	fromEmail := strings.TrimSpace(s.FromEmail)
	if fromEmail == "" {
		fromEmail = "sender@local"
	}
	d := &remote.MailDetail{
		ID:        s.ID,
		Subject:   s.EffectiveSubject(),
		FromName:  s.EffectiveFrom(),
		FromEmail: fromEmail,
		To:        "",
		CreatedAt: s.CreatedAt,
	}
	body := BuildRFC822(d, []byte("<html><body><p>"+htmlEsc(s.Snippet)+"</p></body></html>"), t, nil)
	flags := m.flagsFor(uid, s)
	return &msgLiteral{
		Uid:   uid,
		Flags: flags,
		Date:  t,
		Size:  uint32(len(body)),
		Body:  body,
	}
}

func htmlEsc(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

func (m *Mailbox) buildLiteral(ctx context.Context, s *remote.MailSummary) (*msgLiteral, error) {
	uid := uint32(s.ID)
	flags := m.flagsFor(uid, s)

	idStr := strconv.Itoa(s.ID)
	raw, err := m.user.cli.DoRaw(ctx, &remote.Request{
		Service: "mail",
		Action:  "view",
		Params: map[string]any{
			"mail_id": idStr,
			"param_1": idStr,
		},
		Token:       m.user.cli.Token(),
		CurrentLang: m.user.mailViewCurrentLang,
	})
	if err != nil {
		return nil, err
	}
	detail, err := remote.ParseMailDetailFromViewBody(raw)
	if err != nil {
		return nil, err
	}
	// Prefer remote fields for flags consistency
	detail.ID = s.ID

	html, err := m.user.cli.DownloadGET(ctx, remote.MailHTMLURL(m.user.cli.MailAssetOrigin(), m.user.cli.Token(), s.ID))
	if err != nil {
		m.user.log.Debug("mail html fetch", "err", err)
		html = []byte{}
	}

	if len(detail.Attaches) > 0 {
		detail.Attachments = append(detail.Attachments, detail.Attaches...)
	}
	mailCreated := strings.TrimSpace(detail.CreatedAt)
	if mailCreated == "" {
		mailCreated = strings.TrimSpace(s.CreatedAt)
	}
	var attParts []attachmentPart
	for _, a := range detail.Attachments {
		blob, err := m.user.cli.GetAttachmentBytes(ctx, a, s.ID, mailCreated)
		if err != nil {
			m.user.log.Warn("attachment download failed", "mail_id", s.ID, "attachment_id", a.ID, "err", err.Error())
			continue
		}
		attParts = append(attParts, attachmentPart{
			fileName:    attachmentDisplayName(a),
			contentType: mimeTypeForAttachment(a),
			data:        blob,
		})
	}

	t := parseTime(detail.CreatedAt)
	body := BuildRFC822(detail, html, t, attParts)
	sz := uint32(len(body))
	if sz == 0 {
		sz = 1
	}
	return &msgLiteral{
		Uid:   uid,
		Flags: flags,
		Date:  t,
		Size:  sz,
		Body:  body,
	}, nil
}

func (m *Mailbox) flagsFor(uid uint32, s *remote.MailSummary) []string {
	f := m.summaryFlags(s)
	m.flagMu.Lock()
	if _, ok := m.pendingDeleted[uid]; ok {
		f = append(f, imap.DeletedFlag)
	}
	m.flagMu.Unlock()
	return f
}

func (m *Mailbox) summaryFlags(s *remote.MailSummary) []string {
	var f []string
	if s.Read {
		f = append(f, imap.SeenFlag)
	}
	if s.Important {
		f = append(f, imap.FlaggedFlag)
	}
	if m.spec.Param1 == "draft" {
		f = append(f, imap.DraftFlag)
	}
	return f
}

func parseTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Now()
	}
	s = strings.Replace(s, " ", "T", 1)
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t, err = time.Parse("2006-01-02T15:04:05", s)
	}
	if err != nil {
		return time.Now()
	}
	return t
}

func (m *Mailbox) SearchMessages(uid bool, criteria *imap.SearchCriteria) ([]uint32, error) {
	sums, err := m.user.loadSummaries(context.Background(), m.spec)
	if err != nil {
		return nil, err
	}
	sort.Slice(sums, func(i, j int) bool { return sums[i].ID < sums[j].ID })
	var out []uint32
	for i := range sums {
		seqNum := uint32(i + 1)
		id := uint32(sums[i].ID)
		lit, err := m.buildLiteral(context.Background(), &sums[i])
		if err != nil {
			lit = m.buildLiteralPreview(&sums[i])
		}
		var test uint32
		if uid {
			test = id
		} else {
			test = seqNum
		}
		ok, err := lit.Match(seqNum, criteria)
		if err != nil || !ok {
			continue
		}
		out = append(out, test)
	}
	return out, nil
}

func (m *Mailbox) CreateMessage(flags []string, date time.Time, body imap.Literal) error {
	ctx := context.Background()
	b, err := readLiteral(body)
	if err != nil {
		return err
	}
	// Draft append
	isDraft := false
	for _, f := range flags {
		if f == imap.DraftFlag {
			isDraft = true
			break
		}
	}
	if !isDraft {
		return fmt.Errorf("APPEND supported for Drafts with \\Draft only in this bridge")
	}
	subj, html, recipients, err := parseMIMEForCompose(b)
	if err != nil {
		return err
	}
	data := map[string]any{
		"subject": subj,
		"message": html,
		"users":   strings.Join(recipients, ","),
		"attaches": []any{},
	}
	draft := true
	resp, err := m.user.cli.Do(ctx, &remote.Request{
		Service: "mail",
		Action:  "message-new",
		Draft:   &draft,
		Data:    data,
		CurrentLang: m.user.mailComposeCurrentLang,
		Token:   m.user.cli.Token(),
	})
	if err != nil {
		return err
	}
	if !remote.MailMutationOK(resp) {
		return fmt.Errorf("draft save: %s", resp.Message)
	}
	m.user.invalidateSpec(m.spec)
	return nil
}

func readLiteral(l imap.Literal) ([]byte, error) {
	return io.ReadAll(l)
}

func parseMIMEForCompose(raw []byte) (subject, html string, to []string, err error) {
	s := string(raw)
	headerDone := false
	var hdrLines, bodyLines []string
	for _, ln := range strings.Split(s, "\n") {
		if !headerDone && strings.TrimSpace(ln) == "" {
			headerDone = true
			continue
		}
		if !headerDone {
			hdrLines = append(hdrLines, ln)
		} else {
			bodyLines = append(bodyLines, ln)
		}
	}
	for _, ln := range hdrLines {
		lower := strings.ToLower(ln)
		if strings.HasPrefix(lower, "subject:") {
			if i := strings.IndexByte(ln, ':'); i >= 0 {
				subject = strings.TrimSpace(ln[i+1:])
			}
		}
		if strings.HasPrefix(lower, "to:") {
			v := ""
			if i := strings.IndexByte(ln, ':'); i >= 0 {
				v = strings.TrimSpace(ln[i+1:])
			}
			for _, p := range strings.Split(v, ",") {
				p = strings.TrimSpace(p)
				if p != "" {
					to = append(to, p)
				}
			}
		}
	}
	html = strings.TrimSpace(strings.Join(bodyLines, "\n"))
	if subject == "" {
		subject = "(no subject)"
	}
	return subject, html, to, nil
}

func (m *Mailbox) UpdateMessagesFlags(uid bool, seqSet *imap.SeqSet, op imap.FlagsOp, flags []string) error {
	ctx := context.Background()
	sums, err := m.user.loadSummaries(context.Background(), m.spec)
	if err != nil {
		return err
	}
	sort.Slice(sums, func(i, j int) bool { return sums[i].ID < sums[j].ID })

	maxSeq, maxUID := mailboxMaxSeqAndUID(sums)

	for i := range sums {
		seqNum := uint32(i + 1)
		id := uint32(sums[i].ID)
		var match uint32
		if uid {
			match = id
		} else {
			match = seqNum
		}
		if !seqSetAddressesMessage(uid, seqSet, match, maxSeq, maxUID) {
			continue
		}
		if err := m.applyFlagOp(ctx, sums[i].ID, op, flags); err != nil {
			return err
		}
	}
	m.user.invalidateSpec(m.spec)
	return nil
}

func (m *Mailbox) applyFlagOp(ctx context.Context, mailID int, op imap.FlagsOp, flags []string) error {
	uid := uint32(mailID)
	for _, f := range flags {
		switch f {
		case imap.SeenFlag:
			if op == imap.AddFlags || op == imap.SetFlags {
				if err := m.mailAction(ctx, mailID, "read", nil); err != nil {
					return err
				}
			}
		case imap.DeletedFlag:
			if op == imap.AddFlags || op == imap.SetFlags {
				// Веб шлёт mail-action delete сразу; многие IMAP-клиенты ставят \Deleted
				// и не присылают EXPUNGE/CLOSE — тогда письмо бы не удалилось на сервере.
				if err := m.mailAction(ctx, mailID, "delete", nil); err != nil {
					return err
				}
				m.flagMu.Lock()
				m.pendingDeleted[uid] = struct{}{}
				m.flagMu.Unlock()
			}
			if op == imap.RemoveFlags {
				m.flagMu.Lock()
				delete(m.pendingDeleted, uid)
				m.flagMu.Unlock()
			}
		case imap.FlaggedFlag:
			if op == imap.AddFlags || op == imap.SetFlags {
				if err := m.mailAction(ctx, mailID, "important", nil); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (m *Mailbox) mailAction(ctx context.Context, id int, action string, value any) error {
	data := map[string]any{
		"id":     strconv.Itoa(id),
		"action": action,
	}
	if value != nil {
		switch v := value.(type) {
		case string:
			data["value"] = v
		default:
			data["value"] = fmt.Sprintf("%v", v)
		}
	}
	resp, err := m.user.cli.Do(ctx, &remote.Request{
		Service:     "mail",
		Action:      "mail-action",
		CurrentLang: m.user.mailViewCurrentLang,
		PageID:      PageIDForMailboxSpec(m.spec),
		Data:        data,
		Token:       m.user.cli.Token(),
	})
	if err != nil {
		return err
	}
	if !remote.MailMutationOK(resp) {
		msg := strings.TrimSpace(remote.ResponseErrMsg(resp))
		if msg == "" {
			msg = strings.TrimSpace(resp.Message)
		}
		return fmt.Errorf("mail-action %s: %s", action, msg)
	}
	return nil
}

func (m *Mailbox) CopyMessages(uid bool, seqSet *imap.SeqSet, destName string) error {
	destSpec, err := m.user.ResolveMailboxSpec(destName)
	if err != nil {
		return err
	}
	ctx := context.Background()
	sums, err := m.user.loadSummaries(context.Background(), m.spec)
	if err != nil {
		return err
	}
	sort.Slice(sums, func(i, j int) bool { return sums[i].ID < sums[j].ID })

	maxSeq, maxUID := mailboxMaxSeqAndUID(sums)

	for i := range sums {
		seqNum := uint32(i + 1)
		id := uint32(sums[i].ID)
		var match uint32
		if uid {
			match = id
		} else {
			match = seqNum
		}
		if !seqSetAddressesMessage(uid, seqSet, match, maxSeq, maxUID) {
			continue
		}
		switch destSpec.Param1 {
		case "tags":
			if err := m.mailAction(ctx, sums[i].ID, "move-to-tag", destSpec.Param2); err != nil {
				return err
			}
		case "folders":
			if err := m.mailAction(ctx, sums[i].ID, "move-to-folder", destSpec.Param2); err != nil {
				return err
			}
		default:
			return fmt.Errorf("COPY to %s: use a tag or user folder from LIST", destName)
		}
	}
	return nil
}

// MoveMessages implements backend.MoveMailbox (RFC 6851 UID MOVE).
// Many clients move mail to Trash via MOVE instead of STORE \Deleted + EXPUNGE.
func (m *Mailbox) MoveMessages(uid bool, seqSet *imap.SeqSet, destName string) error {
	destSpec, err := m.user.ResolveMailboxSpec(destName)
	if err != nil {
		return err
	}
	if destSpec.Param1 == m.spec.Param1 && destSpec.Param2 == m.spec.Param2 {
		return nil
	}

	ctx := context.Background()
	sums, err := m.user.loadSummaries(ctx, m.spec)
	if err != nil {
		return err
	}
	sort.Slice(sums, func(i, j int) bool { return sums[i].ID < sums[j].ID })

	maxSeq, maxUID := mailboxMaxSeqAndUID(sums)

	for i := range sums {
		seqNum := uint32(i + 1)
		id := uint32(sums[i].ID)
		var match uint32
		if uid {
			match = id
		} else {
			match = seqNum
		}
		if !seqSetAddressesMessage(uid, seqSet, match, maxSeq, maxUID) {
			continue
		}
		switch destSpec.Param1 {
		case "trash":
			// Same as web «в корзину»: mail-action delete with source mailbox page_id.
			if err := m.mailAction(ctx, sums[i].ID, "delete", nil); err != nil {
				return err
			}
		case "tags":
			if err := m.mailAction(ctx, sums[i].ID, "move-to-tag", destSpec.Param2); err != nil {
				return err
			}
		case "folders":
			if err := m.mailAction(ctx, sums[i].ID, "move-to-folder", destSpec.Param2); err != nil {
				return err
			}
		default:
			return fmt.Errorf("MOVE to %q: not supported (use Trash or a user folder/tag)", destName)
		}
	}

	m.user.invalidateSpec(m.spec)
	m.user.invalidateSpec(destSpec)
	return nil
}

func (m *Mailbox) Expunge() error {
	ctx := context.Background()
	m.flagMu.Lock()
	ids := make([]uint32, 0, len(m.pendingDeleted))
	for u := range m.pendingDeleted {
		ids = append(ids, u)
	}
	m.flagMu.Unlock()

	for _, id := range ids {
		if err := m.mailAction(ctx, int(id), "delete", nil); err != nil {
			return err
		}
		m.flagMu.Lock()
		delete(m.pendingDeleted, id)
		m.flagMu.Unlock()
	}
	m.user.invalidateSpec(m.spec)
	return nil
}

func mailboxMaxSeqAndUID(sums []remote.MailSummary) (maxSeq, maxUID uint32) {
	maxSeq = uint32(len(sums))
	for _, s := range sums {
		u := uint32(s.ID)
		if u > maxUID {
			maxUID = u
		}
	}
	return maxSeq, maxUID
}

// seqSetAddressesMessage extends imap.SeqSet.Contains for RFC-style dynamic sets.
// For "UID FETCH *" the set is dynamic but Contains(non-zero UID) is always false
// (see go-imap seqset); the largest UID must still match (RFC 3501 seq-number *).
func seqSetAddressesMessage(uid bool, seqSet *imap.SeqSet, matchID, maxSeq, maxUID uint32) bool {
	if seqSet == nil || seqSet.Empty() {
		return false
	}
	if matchID == 0 {
		return false
	}
	if seqSet.Contains(matchID) {
		return true
	}
	if !seqSet.Dynamic() {
		return false
	}
	limit := maxUID
	if !uid {
		limit = maxSeq
	}
	return limit != 0 && matchID == limit
}

// --- user summary loading ---

func (u *User) loadSummaries(ctx context.Context, spec MailboxSpec) ([]remote.MailSummary, error) {
	key := specCacheKey(spec)
	u.cacheMu.Lock()
	ent := u.listCache[key]
	if ent != nil && time.Since(ent.at) < summaryCacheTTL {
		out := append([]remote.MailSummary(nil), ent.summaries...)
		u.cacheMu.Unlock()
		return out, nil
	}
	u.cacheMu.Unlock()

	var all []remote.MailSummary
	page := 1
	for {
		req := &remote.Request{
			Service:       "mail",
			Action:        "default",
			Params:        paramsFromSpec(spec),
			Page:          page,
			Token:         u.cli.Token(),
			CurrentLang:   u.mailDefaultActionCurrentLang,
		}
		resp, err := u.cli.Do(ctx, req)
		if err != nil {
			return nil, err
		}
		_ = remote.MergeMailListFields(resp)
		if !remote.MailListResponseOK(resp) {
			// If total.count was missing/wrong we may request page 2+; API sometimes
			// errors once past the last page — keep data already loaded on page 1.
			if page > 1 && len(all) > 0 {
				break
			}
			return nil, fmt.Errorf("list mail: %s", remote.ResponseErrMsg(resp))
		}
		all = append(all, resp.Mails...)
		if len(resp.Mails) == 0 {
			break
		}
		if len(all) >= u.maxList {
			break
		}
		if resp.Total != nil && resp.Total.Count > 0 && len(all) >= resp.Total.Count {
			break
		}
		page++
		if page > 500 {
			break
		}
	}

	u.cacheMu.Lock()
	u.listCache[key] = &summaryCache{at: time.Now(), summaries: append([]remote.MailSummary(nil), all...), spec: spec}
	u.cacheMu.Unlock()
	return all, nil
}
