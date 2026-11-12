package sftpclient

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"

	"github.com/pkg/sftp"
)

// LocalDialer returns a Dialer for a folder on this computer.
//
// The folder is served by an in-process SFTP server over an in-memory pipe,
// so the relay engine reads local files through exactly the same code path —
// and with the same safety checks — as files on a remote source. Paths are
// relative to dir. With readOnly set, nothing in the folder can be changed
// through the session.
func LocalDialer(dir string, readOnly bool) Dialer {
	return func(context.Context) (*sftp.Client, io.Closer, error) {
		info, err := os.Stat(dir)
		if err != nil {
			return nil, nil, err
		}
		if !info.IsDir() {
			return nil, nil, fmt.Errorf("%s is not a folder", dir)
		}
		near, far := net.Pipe()
		opts := []sftp.ServerOption{sftp.WithServerWorkingDirectory(dir)}
		if readOnly {
			opts = append(opts, sftp.ReadOnly())
		}
		srv, err := sftp.NewServer(far, opts...)
		if err != nil {
			_ = near.Close()
			_ = far.Close()
			return nil, nil, err
		}
		go func() { _ = srv.Serve() }()
		cl, err := sftp.NewClientPipe(near, near)
		if err != nil {
			_ = near.Close()
			_ = srv.Close()
			return nil, nil, err
		}
		return cl, closerFunc(func() error {
			_ = near.Close()
			return srv.Close()
		}), nil
	}
}

type closerFunc func() error

func (f closerFunc) Close() error { return f() }
