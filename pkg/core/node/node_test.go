package node_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/nanostack-dev/echopoint-runner/pkg/core/node"
	"github.com/nanostack-dev/echopoint-runner/pkg/core/value"
	"github.com/nanostack-dev/echopoint-runner/pkg/spi"
)

type testCfg struct {
	node.Base

	V int `json:"v"`
}

// TestRegisterDecode proves a node is registered by a typed function, decoded
// into its Cfg, and run — the whole authoring surface.
func TestRegisterDecode(t *testing.T) {
	const kind spi.Kind = "test-x"
	node.Register(kind, func(_ context.Context, cfg testCfg, _ value.Value, _ node.Runtime) (node.Result, error) {
		return node.Result{Outputs: value.Map{"v": value.Of(cfg.V)}}, nil
	})

	b, err := node.Decode(kind, json.RawMessage(`{"id":"n","v":9}`))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if b.Base.ID != "n" || b.Kind != kind {
		t.Fatalf("bound: %+v", b.Base)
	}
	res, err := b.Run(context.Background(), value.Value{}, node.Runtime{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if i, _ := res.Outputs["v"].Int(); i != 9 {
		t.Fatalf("v=%v", i)
	}
}

func TestDecodeUnknownKind(t *testing.T) {
	if _, err := node.Decode(spi.Kind("nope-kind"), json.RawMessage(`{}`)); err == nil {
		t.Fatal("unknown kind should error")
	}
}

func TestCodedError(t *testing.T) {
	err := node.UserErrf("REQUEST_FAILED", "boom %d", 1)
	if node.CodeOf(err) != "REQUEST_FAILED" {
		t.Fatalf("code=%q", node.CodeOf(err))
	}
	if !node.IsUser(err) {
		t.Fatal("should be a user error")
	}
	if node.CodeOf(errors.New("x")) != "" {
		t.Fatal("runner fault should have empty code")
	}
	if node.CodeOf(node.ErrUser) != "USER_ERROR" {
		t.Fatal("uncoded user error should be USER_ERROR")
	}
}
