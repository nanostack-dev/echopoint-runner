package node

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/nanostack-dev/echopoint-runner/pkg/extractors"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

const (
	emailRecentMessagesKept = 5

	outputReceived   = "received"
	outputReceivedAt = "received_at"
	outputFrom       = "from"
	outputSubject    = "subject"
	outputEnvelopeTo = "envelope_to"
	capabilityBody   = "body"
)

// textURLPattern finds the URLs of a text part. It stops at space, quotes and
// angle brackets; trailing punctuation is trimmed after the match.
var textURLPattern = regexp.MustCompile(`(?i)https?://[^\s<>"'` + "`" + `]+`)

// emailOutputNames are the outputs of a matched message, in report order.
func emailOutputNames() []string {
	return []string{
		"id", "message_id", outputFrom, "to", "envelope_from", outputEnvelopeTo, outputSubject,
		"text", "html", "links", "first_link", "code", outputReceivedAt,
	}
}

// inboxMessage is one email of the run inbox as the runner read endpoint
// returns it.
type inboxMessage struct {
	ID           string    `json:"id"`
	EnvelopeFrom string    `json:"envelope_from"`
	EnvelopeTo   string    `json:"envelope_to"`
	From         string    `json:"from"`
	To           string    `json:"to"`
	Subject      string    `json:"subject"`
	MessageID    string    `json:"message_id"`
	Text         *string   `json:"text"`
	HTML         *string   `json:"html"`
	ReceivedAt   time.Time `json:"received_at"`
}

type inboxMessageList struct {
	Items []inboxMessage `json:"items"`
}

// emailPatterns are the node settings that shape what a message yields.
type emailPatterns struct {
	link *regexp.Regexp
	code *regexp.Regexp
}

// readEmail is a message with what the wait derives from it: the readable
// text, the links and the code. Assertions and outputs read this view.
type readEmail struct {
	inboxMessage

	text  string
	html  string
	links []string
	code  string
}

func readMessage(message inboxMessage, patterns emailPatterns) readEmail {
	read := readEmail{inboxMessage: message}
	if message.HTML != nil {
		read.html = *message.HTML
	}
	switch {
	case message.Text != nil && strings.TrimSpace(*message.Text) != "":
		read.text = *message.Text
	case read.html != "":
		read.text = extractors.HTMLText(read.html)
	}
	read.links = emailLinks(read.html, read.text, patterns.link)
	if patterns.code != nil {
		if match := patterns.code.FindStringSubmatch(read.text); match != nil {
			read.code = match[extractors.FirstGroup(patterns.code)]
		}
	}
	return read
}

// emailLinks lists the http and https links of a message in document order,
// without duplicates: the hrefs of its HTML, or the URLs of its text when it
// has no HTML. A link pattern keeps the links it matches.
func emailLinks(htmlPart, text string, pattern *regexp.Regexp) []string {
	var candidates []string
	if htmlPart != "" {
		candidates = extractors.HTMLLinks(htmlPart)
	} else {
		for _, found := range textURLPattern.FindAllString(text, -1) {
			candidates = append(candidates, strings.TrimRight(found, ".,;:!?)]}"))
		}
	}
	links := []string{}
	seen := map[string]bool{}
	for _, link := range candidates {
		parsed, err := url.Parse(link)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			continue
		}
		if seen[link] || (pattern != nil && !pattern.MatchString(link)) {
			continue
		}
		seen[link] = true
		links = append(links, link)
	}
	return links
}

func (m readEmail) eventID() string {
	return m.ID
}

// outputs are what a matched message publishes. Every key is always present,
// so a reference to code or first_link never fails on a message without one.
func (m readEmail) outputs() map[string]any {
	links := make([]any, len(m.links))
	firstLink := ""
	for i, link := range m.links {
		links[i] = link
	}
	if len(m.links) > 0 {
		firstLink = m.links[0]
	}
	return map[string]any{
		"id":             m.ID,
		"message_id":     m.MessageID,
		outputFrom:       m.From,
		"to":             m.To,
		"envelope_from":  m.EnvelopeFrom,
		outputEnvelopeTo: m.EnvelopeTo,
		outputSubject:    m.Subject,
		"text":           m.text,
		"html":           m.html,
		"links":          links,
		"first_link":     firstLink,
		"code":           m.code,
		outputReceivedAt: formatReceivedAt(m.ReceivedAt),
	}
}

// summary is what a failed wait reports of a message it saw: who sent it,
// to whom, and its subject, never its body.
func (m readEmail) summary() map[string]any {
	return map[string]any{
		"id":             m.ID,
		outputFrom:       m.From,
		"to":             m.To,
		outputEnvelopeTo: m.EnvelopeTo,
		outputSubject:    m.Subject,
		outputReceivedAt: formatReceivedAt(m.ReceivedAt),
	}
}

func formatReceivedAt(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return at.UTC().Format(time.RFC3339Nano)
}

// assertionContext lets assertions and outputs read a message: json_path
// reads its outputs, body and regex read its text, html reads its HTML part,
// header reads From, To, Subject and Message-Id.
func (m readEmail) assertionContext() extractors.ResponseContext {
	headers := http.Header{}
	headers.Set("From", m.From)
	headers.Set("To", m.To)
	headers.Set("Subject", m.Subject)
	headers.Set("Message-Id", m.MessageID)
	return emailContext{parsed: m.outputs(), text: []byte(m.text), html: []byte(m.html), headers: headers}
}

type emailContext struct {
	parsed  map[string]any
	text    []byte
	html    []byte
	headers http.Header
}

func (c emailContext) HasCapability(capability string) bool {
	switch capability {
	case headersCapability, capabilityBody, "parsed_body":
		return true
	default:
		return false
	}
}

func (c emailContext) GetHeader(key string) string {
	return c.headers.Get(key)
}

func (c emailContext) Headers() http.Header {
	return c.headers
}

func (c emailContext) GetParsedBody() any {
	return c.parsed
}

func (c emailContext) GetRawBody() []byte {
	return c.text
}

func (c emailContext) GetHTML() []byte {
	return c.html
}

func fetchInboxMessages(
	ctx context.Context, client *http.Client, messagesURL, token string,
) ([]inboxMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, messagesURL, nil)
	if err != nil {
		return nil, emailWaitError("could not build the inbox messages request", err)
	}
	req.Header.Set("X-Job-Token", token)
	req.Header.Set("Accept", "application/json")
	spi.TraceContextFromContext(ctx).SetHeaders(req.Header)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("inbox messages get: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("inbox messages read: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		var list inboxMessageList
		if unmarshalErr := json.Unmarshal(raw, &list); unmarshalErr != nil {
			return nil, emailWaitError("inbox messages response is not valid JSON", unmarshalErr)
		}
		return list.Items, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, emailWaitError("the job token was rejected", nil)
	case http.StatusNotFound:
		return nil, emailWaitError("the run inbox was not found", nil)
	default:
		return nil, fmt.Errorf("inbox messages get status %d", resp.StatusCode)
	}
}

func emailWaitError(message string, cause error) error {
	return spi.NewUserError(emailWaitFailedCode, message, cause)
}
