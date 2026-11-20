package host

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"time"
)

// CommandOutput runs a host integration with caller-owned cancellation and a
// finite stdout buffer. Stderr remains private to the subprocess.
func CommandOutput(ctx context.Context, limit int, executable string, args ...string) ([]byte, error) {
	output := &commandBuffer{limit: limit}
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Stdout = output
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	return output.buffer.Bytes(), err
}

type commandBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *commandBuffer) Write(data []byte) (int, error) {
	if len(data) > b.limit-b.buffer.Len() {
		return 0, errors.New("host command output exceeds its limit")
	}
	return b.buffer.Write(data)
}
