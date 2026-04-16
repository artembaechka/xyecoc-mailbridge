package remote

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// ExtractToken reads JWT from authorization / refresh-token responses (nested data.token or data as string).
func ExtractToken(resp *Response) (string, error) {
	if len(resp.Data) == 0 {
		return "", fmt.Errorf("empty data field")
	}
	var inner struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(resp.Data, &inner); err == nil && inner.Token != "" {
		return inner.Token, nil
	}
	var s string
	if err := json.Unmarshal(resp.Data, &s); err == nil && s != "" {
		return s, nil
	}
	return "", fmt.Errorf("no token in data")
}

// ParseMailDetail decodes mail view response body.
func ParseMailDetail(resp *Response) (*MailDetail, error) {
	if resp.Status != 1 {
		return nil, fmt.Errorf("view failed: %s", resp.Message)
	}
	var d MailDetail
	if len(resp.Data) > 0 {
		if err := json.Unmarshal(resp.Data, &d); err != nil {
			return nil, err
		}
		return &d, nil
	}
	return nil, fmt.Errorf("empty view data")
}

// ParseMailDetailFromViewBody parses the full HTTP JSON body of mail/view.
// The API may put the letter in `data`, in `data.mail`, or at the root (then json.Unmarshal into Response drops unknown fields).
func ParseMailDetailFromViewBody(b []byte) (*MailDetail, error) {
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return nil, fmt.Errorf("empty view body")
	}
	var st struct {
		Status int `json:"status"`
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, err
	}
	if st.Status != 1 {
		var errBody struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(b, &errBody)
		return nil, fmt.Errorf("view failed: %s", errBody.Message)
	}

	var withData struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(b, &withData); err != nil {
		return nil, err
	}
	if len(withData.Data) > 0 && string(withData.Data) != "null" {
		var wrap struct {
			Mail        MailDetail   `json:"mail"`
			Attachments []Attachment `json:"attachments"`
			Attaches    []Attachment `json:"attaches"`
		}
		if err := json.Unmarshal(withData.Data, &wrap); err == nil && wrap.Mail.ID != 0 {
			m := wrap.Mail
			if len(m.Attachments) == 0 {
				m.Attachments = append(wrap.Attachments, wrap.Attaches...)
			}
			return &m, nil
		}
		var inner MailDetail
		if err := json.Unmarshal(withData.Data, &inner); err == nil && inner.ID != 0 {
			return &inner, nil
		}
		if err := json.Unmarshal(withData.Data, &inner); err == nil && (inner.Message != "" || inner.Subject != "" || inner.FromEmail != "") {
			return &inner, nil
		}
	}

	var root MailDetail
	if err := json.Unmarshal(b, &root); err != nil {
		return nil, err
	}
	if root.ID != 0 {
		return &root, nil
	}
	return nil, fmt.Errorf("could not parse mail/view payload")
}

// MergeMailListFields: some deployments may nest list payload under data.
func MergeMailListFields(resp *Response) error {
	if len(resp.Mails) > 0 {
		return nil
	}
	if len(resp.Data) == 0 {
		return nil
	}
	var nested struct {
		Mails []MailSummary `json:"mails"`
		Total *TotalCount   `json:"total"`
	}
	if err := json.Unmarshal(resp.Data, &nested); err != nil {
		return nil
	}
	if len(nested.Mails) > 0 {
		resp.Mails = nested.Mails
	}
	if nested.Total != nil {
		resp.Total = nested.Total
	}
	return nil
}
