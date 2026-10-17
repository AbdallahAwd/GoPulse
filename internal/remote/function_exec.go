package remote

import (
	"context"
	"fmt"
	"io"
)

// RunFunctionContext uses a dedicated SSH connection. Writers must be bounded
// and safe for concurrent stdout/stderr writes. Closing the connection on
// cancellation also interrupts session creation and stdin transfers.
func (c *SSHClient) RunFunctionContext(ctx context.Context, command string, input io.Reader, output io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	if err := c.Connect(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		c.Close()
		return err
	}
	c.mu.Lock()
	client := c.client
	c.mu.Unlock()
	if client == nil {
		return fmt.Errorf("SSH disconnected")
	}
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	session.Stdin = input
	session.Stdout = output
	session.Stderr = output
	err = session.Run(command)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
