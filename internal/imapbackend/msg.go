package imapbackend

import (
	"bufio"
	"bytes"
	"io"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/backend/backendutil"
	"github.com/emersion/go-message"
	"github.com/emersion/go-message/textproto"
)

// msgLiteral holds RFC822 bytes for go-imap Fetch.
type msgLiteral struct {
	Uid   uint32
	Flags []string
	Date  time.Time
	Size  uint32
	Body  []byte
}

func (m *msgLiteral) entity() (*message.Entity, error) {
	return message.Read(bytes.NewReader(m.Body))
}

func (m *msgLiteral) headerAndBody() (textproto.Header, io.Reader, error) {
	body := bufio.NewReader(bytes.NewReader(m.Body))
	hdr, err := textproto.ReadHeader(body)
	return hdr, body, err
}

func (m *msgLiteral) Fetch(seqNum uint32, items []imap.FetchItem) (*imap.Message, error) {
	fetched := imap.NewMessage(seqNum, items)
	for _, item := range items {
		switch item {
		case imap.FetchEnvelope:
			hdr, _, _ := m.headerAndBody()
			fetched.Envelope, _ = backendutil.FetchEnvelope(hdr)
		case imap.FetchBody, imap.FetchBodyStructure:
			hdr, body, _ := m.headerAndBody()
			fetched.BodyStructure, _ = backendutil.FetchBodyStructure(hdr, body, item == imap.FetchBodyStructure)
		case imap.FetchFlags:
			fetched.Flags = m.Flags
		case imap.FetchInternalDate:
			fetched.InternalDate = m.Date
		case imap.FetchRFC822Size:
			fetched.Size = m.Size
		case imap.FetchUid:
			fetched.Uid = m.Uid
		default:
			section, err := imap.ParseBodySectionName(item)
			if err != nil {
				break
			}
			body := bufio.NewReader(bytes.NewReader(m.Body))
			hdr, err := textproto.ReadHeader(body)
			if err != nil {
				return nil, err
			}
			l, _ := backendutil.FetchBodySection(hdr, body, section)
			if fetched.Body == nil {
				fetched.Body = make(map[*imap.BodySectionName]imap.Literal)
			}
			fetched.Body[section] = l
		}
	}
	return fetched, nil
}

func (m *msgLiteral) Match(seqNum uint32, c *imap.SearchCriteria) (bool, error) {
	e, err := m.entity()
	if err != nil {
		return false, err
	}
	return backendutil.Match(e, seqNum, m.Uid, m.Date, m.Flags, c)
}
