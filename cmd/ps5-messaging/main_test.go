package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// The service must refuse to start without its durable backend unless PS5_DEV_MEMORY=1.
func TestRefusesToStartWithoutBackend(t *testing.T) {
	if os.Getenv("PS5_RUN_MAIN") == "1" {
		main()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestRefusesToStartWithoutBackend")
	cmd.Env = []string{"PS5_RUN_MAIN=1", "PS5_TOKEN_KEY=0123456789abcdef0123456789abcdef", "PS5_ADDR=127.0.0.1:0"}
	out, err := cmd.CombinedOutput()
	ee, ok := err.(*exec.ExitError)
	if !ok || ee.ExitCode() != 2 || !strings.Contains(string(out), "PS5_DATABASE_URL") {
		t.Fatalf("exit %v, output: %s", err, out)
	}
}
