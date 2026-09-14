package cli_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jsazanami-del/pg2protobuf-generator/internal/cli"
)

func TestInitRefuseOverwrite(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, ".pg2proto.yaml")
	ctx := context.Background()

	run := func(args ...string) error {
		cmd := cli.NewRoot(ctx)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs(args)
		return cmd.ExecuteContext(ctx)
	}

	if err := run("init", "--config", cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cfg); err != nil {
		t.Fatal(err)
	}
	if err := run("init", "--config", cfg); err == nil {
		t.Fatal("expected overwrite error")
	}
	if err := run("init", "--config", cfg, "--force"); err != nil {
		t.Fatal(err)
	}
}
