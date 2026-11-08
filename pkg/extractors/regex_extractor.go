package extractors

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

var errNoText = errors.New("regex extractor: the result has no body")

// RegexExtractor returns the first match of Pattern in the raw body: the text
// of an email, the body of a response. Group picks a capture group; without
// it, the value is group 1 when the pattern has a group, else the whole match.
type RegexExtractor struct {
	Pattern string `json:"pattern"`
	Group   *int   `json:"group,omitempty"`
}

func (e RegexExtractor) Extract(ctx ResponseContext) (any, error) {
	pattern, err := regexp.Compile(e.Pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex '%s': %w", e.Pattern, err)
	}
	group := e.group(pattern)
	if group < 0 || group > pattern.NumSubexp() {
		return nil, fmt.Errorf("regex '%s' has no group %d", e.Pattern, group)
	}
	reader, ok := ctx.(ParsedBodyReader)
	if !ok || reader.GetRawBody() == nil {
		return nil, errNoText
	}
	match := pattern.FindSubmatch(reader.GetRawBody())
	if match == nil {
		return nil, fmt.Errorf("regex '%s' did not match", e.Pattern)
	}
	return string(match[group]), nil
}

func (e RegexExtractor) GetType() spi.ExtractorType {
	return spi.ExtractorTypeRegex
}

func (e RegexExtractor) group(pattern *regexp.Regexp) int {
	if e.Group != nil {
		return *e.Group
	}
	return FirstGroup(pattern)
}

// FirstGroup is the capture group a pattern's value comes from: 1 when the
// pattern has a group, else 0, the whole match.
func FirstGroup(pattern *regexp.Regexp) int {
	return min(1, pattern.NumSubexp())
}
