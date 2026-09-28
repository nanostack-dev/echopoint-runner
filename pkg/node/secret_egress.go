package node

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/nanostack-dev/echopoint-runner/pkg/clouddial"
	"github.com/nanostack-dev/echopoint-runner/pkg/secretegress"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

const maxRedirects = 10

func refuseSecretEgress(ctx spi.ExecutionContext, rawURL string, headers map[string]string, body any) error {
	err := secretegress.Check(ctx.SecretValues, ctx.SecretHosts, rawURL, headers, body)
	if userErr := secretEgressUserError(err); userErr != nil {
		return userErr
	}
	return err
}

func outboundHTTPClient(ctx spi.ExecutionContext, body any) *http.Client {
	client := nodeHTTPClient()
	if clouddial.Job() || len(ctx.SecretValues) == 0 {
		return client
	}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fmt.Errorf("stopped after %d redirects", maxRedirects)
		}
		var hopBody any
		if req.Body != nil {
			hopBody = body
		}
		return secretegress.Check(
			ctx.SecretValues,
			ctx.SecretHosts,
			req.URL.String(),
			headerValues(req.Header),
			hopBody,
		)
	}
	return client
}

func headerValues(header http.Header) map[string]string {
	flat := make(map[string]string, len(header))
	for key, values := range header {
		flat[key] = strings.Join(values, ",")
	}
	return flat
}

func secretEgressUserError(err error) *spi.UserError {
	if err == nil {
		return nil
	}
	if !errors.Is(err, secretegress.ErrInURL) && !errors.Is(err, secretegress.ErrHost) {
		return nil
	}
	code := "SECRET_HOST_NOT_ALLOWED"
	if errors.Is(err, secretegress.ErrInURL) {
		code = "SECRET_IN_URL"
	}
	return spi.NewUserError(code, secretegress.PublicMessage(err), nil)
}
