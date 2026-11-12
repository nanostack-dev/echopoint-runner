package extractors

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"

	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

var errNoHTML = errors.New("html extractor: the result has no HTML")

// HTMLReader provides the HTML a context holds when its raw body is something
// else, such as the HTML part of an email whose body reads as text.
type HTMLReader interface {
	GetHTML() []byte
}

// HTMLExtractor reads an HTML document with a CSS selector. It parses the way a
// browser does, so the unclosed tags and stray markup of real email and web
// pages never fail the parse. The value is the text of the first matching
// element, or its Attribute when set; with All, a list over every match.
type HTMLExtractor struct {
	Selector  string `json:"selector"`
	Attribute string `json:"attribute,omitempty"`
	All       bool   `json:"all,omitempty"`
}

func (e HTMLExtractor) Extract(ctx ResponseContext) (any, error) {
	selector, err := cascadia.Compile(e.Selector)
	if err != nil {
		return nil, fmt.Errorf("invalid CSS selector '%s': %w", e.Selector, err)
	}
	raw := htmlOf(ctx)
	if len(raw) == 0 {
		return nil, errNoHTML
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("html extractor: %w", err)
	}
	selection := doc.FindMatcher(selector)
	values := make([]any, 0, selection.Length())
	selection.EachWithBreak(func(_ int, element *goquery.Selection) bool {
		if value, found := e.pick(element); found {
			values = append(values, value)
		}
		return e.All
	})
	if e.All {
		return values, nil
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("CSS selector '%s' did not match any element", e.Selector)
	}
	return values[0], nil
}

func (e HTMLExtractor) GetType() spi.ExtractorType {
	return spi.ExtractorTypeHTML
}

// Target names the selector the extractor reads, with its attribute as selector@attribute.
func (e HTMLExtractor) Target() string {
	if e.Attribute == "" || e.Selector == "" {
		return e.Selector
	}
	return e.Selector + "@" + e.Attribute
}

func (e HTMLExtractor) pick(element *goquery.Selection) (string, bool) {
	if e.Attribute == "" {
		return collapseSpace(element.Text()), true
	}
	value, found := element.Attr(e.Attribute)
	return strings.TrimSpace(value), found
}

func htmlOf(ctx ResponseContext) []byte {
	if reader, ok := ctx.(HTMLReader); ok {
		return reader.GetHTML()
	}
	if reader, ok := ctx.(ParsedBodyReader); ok {
		return reader.GetRawBody()
	}
	return nil
}

// HTMLText renders an HTML document as the text a reader sees: no script,
// style or head content, a line break after each block element and <br>, and
// runs of spaces collapsed.
func HTMLText(raw string) string {
	root, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		return ""
	}
	var out strings.Builder
	writeText(&out, root)
	lines := strings.Split(out.String(), "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if line = collapseSpace(line); line != "" {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

func writeText(out *strings.Builder, n *html.Node) {
	switch n.Type {
	case html.TextNode:
		out.WriteString(n.Data)
		return
	case html.ElementNode:
		switch n.Data {
		case "script", "style", "head", "title", "noscript", "template":
			return
		case "br":
			out.WriteByte('\n')
			return
		}
	case html.ErrorNode, html.DocumentNode, html.CommentNode, html.DoctypeNode, html.RawNode:
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		writeText(out, child)
	}
	if n.Type == html.ElementNode && blockElements[n.Data] {
		out.WriteByte('\n')
	}
}

//nolint:gochecknoglobals // read-only lookup set
var blockElements = map[string]bool{
	"p": true, "div": true, "tr": true, "li": true, "table": true, "h1": true, "h2": true,
	"h3": true, "h4": true, "h5": true, "h6": true, "ul": true, "ol": true, "blockquote": true,
	"section": true, "article": true, "header": true, "footer": true, "center": true, "hr": true,
	"td": true, "th": true, "pre": true, "address": true, "dd": true, "dt": true,
}

// HTMLLinks lists the href of every <a> element in document order, with
// entities decoded and surrounding space trimmed.
func HTMLLinks(raw string) []string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(raw))
	if err != nil {
		return nil
	}
	var links []string
	doc.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		href, _ := anchor.Attr("href")
		if href = strings.TrimSpace(href); href != "" {
			links = append(links, href)
		}
	})
	return links
}

func collapseSpace(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
