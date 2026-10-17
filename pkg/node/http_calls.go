package node

import (
	"errors"
	"io"
	"net/http"

	"github.com/nanostack-dev/echopoint-runner/pkg/httpcall"
)

type callTransport struct {
	inner       http.RoundTripper
	nodeID      string
	urlTemplate string
	stream      bool
}

func recordingClient(nodeID, urlTemplate string, stream bool) *http.Client {
	client := nodeHTTPClient()
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	client.Transport = callTransport{inner: transport, nodeID: nodeID, urlTemplate: urlTemplate, stream: stream}
	return client
}

func (t callTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := httpcall.Wait(req.Context()); err != nil {
		return nil, err
	}
	call, err := httpcall.Start(req.Context(), httpcall.Target{
		NodeID: t.nodeID, Method: req.Method, URL: req.URL.String(), URLTemplate: t.urlTemplate,
	})
	if err != nil {
		return nil, err
	}
	outcome := httpcall.Outcome{}
	if !t.stream {
		outcome.ResponseBytes = new(int64)
	}
	resp, err := t.inner.RoundTrip(req)
	if err != nil {
		outcome.TransportError = callErrorClass(req.URL.String(), err)
		call.Finish(outcome)
		return resp, err
	}
	outcome.StatusCode = resp.StatusCode
	resp.Body = &callBody{ReadCloser: resp.Body, call: call, outcome: outcome}
	return resp, nil
}

type callBody struct {
	io.ReadCloser

	call    *httpcall.Call
	outcome httpcall.Outcome
}

func (b *callBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if b.outcome.ResponseBytes != nil {
		*b.outcome.ResponseBytes += int64(n)
	}
	if errors.Is(err, io.EOF) {
		b.finish()
	}
	return n, err
}

// Close finishes a call whose body was not read to the end. A body read to the
// end finished at EOF, so the node's own processing is not counted.
func (b *callBody) Close() error {
	b.finish()
	return b.ReadCloser.Close()
}

func (b *callBody) finish() {
	b.call.Finish(b.outcome)
	b.call = nil
}
