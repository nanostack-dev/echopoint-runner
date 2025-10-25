package extractors_test

import (
	"testing"

	"github.com/nanostack-dev/echopoint-flow-engine/pkg/extractors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestXMLPathExtractor_GetType(t *testing.T) {
	extractor := extractors.XMLPathExtractor{Path: "/response/status"}
	assert.Equal(t, extractors.ExtractorTypeXMLPath, extractor.GetType())
}

func TestXMLPathExtractor_Extract_SimpleElement(t *testing.T) {
	t.Skip("TODO: Implement XPath extraction logic")

	extractor := extractors.XMLPathExtractor{Path: "/response/status"}
	xmlResponse := `
		<response>
			<status>success</status>
			<code>200</code>
		</response>
	`

	result, err := extractor.Extract(xmlResponse)

	require.NoError(t, err)
	assert.Equal(t, "success", result)
}

func TestXMLPathExtractor_Extract_NestedElement(t *testing.T) {
	t.Skip("TODO: Implement XPath extraction logic")

	extractor := extractors.XMLPathExtractor{Path: "/response/user/name"}
	xmlResponse := `
		<response>
			<user>
				<name>John Doe</name>
				<age>30</age>
			</user>
		</response>
	`

	result, err := extractor.Extract(xmlResponse)

	require.NoError(t, err)
	assert.Equal(t, "John Doe", result)
}

func TestXMLPathExtractor_Extract_Attribute(t *testing.T) {
	t.Skip("TODO: Implement XPath extraction logic")

	extractor := extractors.XMLPathExtractor{Path: "/response/user/@id"}
	xmlResponse := `
		<response>
			<user id="user-123">
				<name>John Doe</name>
			</user>
		</response>
	`

	result, err := extractor.Extract(xmlResponse)

	require.NoError(t, err)
	assert.Equal(t, "user-123", result)
}

func TestXMLPathExtractor_Extract_ArrayElement(t *testing.T) {
	t.Skip("TODO: Implement XPath extraction logic")

	extractor := extractors.XMLPathExtractor{Path: "/response/orders/order[1]/id"}
	xmlResponse := `
		<response>
			<orders>
				<order>
					<id>order-123</id>
					<total>100</total>
				</order>
				<order>
					<id>order-456</id>
					<total>200</total>
				</order>
			</orders>
		</response>
	`

	result, err := extractor.Extract(xmlResponse)

	require.NoError(t, err)
	assert.Equal(t, "order-123", result)
}

func TestXMLPathExtractor_Extract_NonexistentPath(t *testing.T) {
	t.Skip("TODO: Implement XPath extraction logic")

	extractor := extractors.XMLPathExtractor{Path: "/response/nonexistent"}
	xmlResponse := `
		<response>
			<status>success</status>
		</response>
	`

	result, err := extractor.Extract(xmlResponse)

	require.Error(t, err)
	assert.Nil(t, result)
}

func TestXMLPathExtractor_Extract_InvalidXML(t *testing.T) {
	t.Skip("TODO: Implement XPath extraction logic")

	extractor := extractors.XMLPathExtractor{Path: "/response/status"}
	xmlResponse := "<invalid xml"

	result, err := extractor.Extract(xmlResponse)

	require.Error(t, err)
	assert.Nil(t, result)
}

func TestXMLPathExtractor_Extract_WithNamespace(t *testing.T) {
	t.Skip("TODO: Implement XPath extraction logic")

	extractor := extractors.XMLPathExtractor{Path: "/soap:Envelope/soap:Body/response/status"}
	xmlResponse := `
		<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
			<soap:Body>
				<response>
					<status>success</status>
				</response>
			</soap:Body>
		</soap:Envelope>
	`

	result, err := extractor.Extract(xmlResponse)

	require.NoError(t, err)
	assert.Equal(t, "success", result)
}
