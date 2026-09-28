package node

import (
	"errors"

	"github.com/nanostack-dev/echopoint-runner/pkg/secretegress"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

func refuseSecretEgress(ctx spi.ExecutionContext, rawURL string, headers map[string]string, body any) error {
	err := secretegress.Check(ctx.SecretValues, ctx.SecretHosts, rawURL, headers, body)
	if err == nil {
		return nil
	}
	if errors.Is(err, secretegress.ErrInURL) {
		return spi.NewUserError("SECRET_IN_URL", err.Error(), nil)
	}
	if errors.Is(err, secretegress.ErrHost) {
		return spi.NewUserError("SECRET_HOST_NOT_ALLOWED", err.Error(), nil)
	}
	return err
}
