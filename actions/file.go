package actions

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/mihaitodor/wormhole/config"
	"github.com/mihaitodor/wormhole/connection"
	"golang.org/x/sync/errgroup"
)

type FileAction struct {
	Src   string
	Dest  string
	Owner string
	Group string
	Mode  string
}

func (*FileAction) GetType() string {
	return "file"
}

// Copies the contents of src to dest on a remote host
func copyFile(sess *connection.Session, timeout time.Duration, src io.Reader, size int64, dest, mode string) error {
	// Instruct the remote scp process that we want to bail out immediately
	// TODO: Log error
	defer sess.CloseStdin()

	_, err := fmt.Fprintln(sess.Stdin(), "C"+mode, size, filepath.Base(dest))
	if err != nil {
		return fmt.Errorf("failed to create remote file: %s", err)
	}

	_, err = io.Copy(sess.Stdin(), src)
	if err != nil {
		return fmt.Errorf("failed to write remote file contents: %s", err)
	}

	_, err = fmt.Fprint(sess.Stdin(), "\x00")
	if err != nil {
		return fmt.Errorf("failed to close remote file: %s", err)
	}

	return nil
}

func (a *FileAction) Run(ctx context.Context, conn *connection.Connection, conf config.Config) error {
	err := conn.Exec(ctx, false, func(sess *connection.Session) (error, *errgroup.Group) {
		f, err := os.Open(filepath.Join(conf.PlaybookFolder, a.Src))
		if err != nil {
			return fmt.Errorf("failed to open source file: %s", err), nil
		}
		stat, err := f.Stat()
		if err != nil {
			return fmt.Errorf("failed to get source file info: %s", err), nil
		}

		// Start scp receiver on the remote host
		err = sess.Start("scp -qt " + filepath.Dir(a.Dest))
		if err != nil {
			return fmt.Errorf("failed to start scp receiver: %s", err), nil
		}

		mode := a.Mode
		if mode == "" {
			mode = "0644"
		}

		var g errgroup.Group
		g.Go(func() error {
			return copyFile(
				sess,
				conf.ExecTimeout,
				f,
				stat.Size(),
				a.Dest,
				mode,
			)
		})
		return nil, &g
	})
	if err != nil {
		return fmt.Errorf("failed to copy file %q: %s", a.Src, err)
	}

	if a.Owner != "" && a.Group != "" {
		err = conn.Exec(ctx, true, func(sess *connection.Session) (error, *errgroup.Group) {
			return sess.Start(
				fmt.Sprintf("chown %s:%s %s", a.Owner, a.Group, a.Dest),
			), nil
		})
		if err != nil {
			return fmt.Errorf(
				"failed to set the file owner on %q to %s:%s: %s",
				a.Dest, a.Owner, a.Group, err,
			)
		}
	}

	return nil
}
