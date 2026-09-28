package node

import (
	"net/http"

	"github.com/nanostack-dev/echopoint-runner/pkg/clouddial"
)

func nodeHTTPClient() *http.Client {
	if !clouddial.Job() {
		return &http.Client{}
	}
	return clouddial.Client()
}
