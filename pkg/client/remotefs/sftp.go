package remotefs

import (
	"context"
	"fmt"
	"net"
	"runtime"
	"sync"
	"time"

	"github.com/datawire/dlib/dcontext"
	"github.com/datawire/dlib/dgroup"
	"github.com/datawire/dlib/dlog"
	"github.com/telepresenceio/telepresence/v2/pkg/client"
	"github.com/telepresenceio/telepresence/v2/pkg/dpipe"
	"github.com/telepresenceio/telepresence/v2/pkg/iputil"
	"github.com/telepresenceio/telepresence/v2/pkg/proc"
)

type sftpMounter struct {
	sync.Mutex
	podWG *sync.WaitGroup
}

func NewSFTPMounter(wg *sync.WaitGroup) Mounter {
	return &sftpMounter{podWG: wg}
}

func (m *sftpMounter) Start(ctx context.Context, id, clientMountPoint, mountPoint, podIP string, port int32) error {
	if iputil.IsIpV6Addr(podIP) {
		ctx = dgroup.WithGoroutineName(ctx, fmt.Sprintf("[/%s]:%d", podIP, port))
	} else {
		ctx = dgroup.WithGoroutineName(ctx, fmt.Sprintf("/%s:%d", podIP, port))
	}

	// The mount is terminated and restarted when the intercept pod changes, so we
	// must set up a wait/done pair here to ensure that this happens synchronously
	m.podWG.Add(1)
	go func() {
		defer m.podWG.Done()

		// Be really sure that the following doesn't happen in parallel using multiple
		// pods for the same intercept. One must die before the next is created.
		m.Lock()
		defer m.Unlock()

		dlog.Infof(ctx, "Mounting SFTP file system for intercept %q (pod %s) at %q", id, podIP, clientMountPoint)
		defer dlog.Infof(ctx, "Unmounting SFTP file system for intercept %q (pod %s) at %q", id, podIP, clientMountPoint)

		// Retry mount in case it gets disconnected
		err := client.Retry(ctx, "sshfs", func(ctx context.Context) error {
			dl := &net.Dialer{Timeout: 3 * time.Second}
			var conn net.Conn
			var err error
			if iputil.IsIpV6Addr(podIP) {
				conn, err = dl.DialContext(ctx, "tcp", fmt.Sprintf("[%s]:%d", podIP, port))
			} else {
				conn, err = dl.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", podIP, port))
			}
			if err != nil {
				return err
			}
			defer conn.Close()
			sshfsArgs := []string{
				"-F", "none", // don't load the user's config file
				"-f", // foreground operation

				// connection settings
				"-C", // compression
				"-oConnectTimeout=10",
				"-oStrictHostKeyChecking=no",     // don't bother checking the host key...
				"-oUserKnownHostsFile=/dev/null", // and since we're not checking it, don't bother remembering it either
				"-o", "slave",                    // Unencrypted via stdin/stdout

				// mount directives
				"-o", "follow_symlinks",
				"-o", "allow_root", // needed to make --docker-run work as docker runs as root
				"localhost:" + mountPoint, // what to mount
				clientMountPoint,          // where to mount it
			}
			exe := "sshfs"
			if runtime.GOOS == "windows" {
				// Use sshfs-win to launch the sshfs
				sshfsArgs = append([]string{"cmd", "-ouid=-1", "-ogid=-1"}, sshfsArgs...)
				exe = "sshfs-win"
			}
			err = dpipe.DPipe(ctx, conn, exe, sshfsArgs...)
			time.Sleep(time.Second)

			// sshfs sometimes leave the mount point in a bad state. This will clean it up
			ctx, cancel := context.WithTimeout(dcontext.WithoutCancel(ctx), time.Second)
			defer cancel()
			_ = proc.CommandContext(ctx, "fusermount", "-uz", clientMountPoint).Run()
			return err
		}, 3*time.Second, 6*time.Second)
		if err != nil {
			dlog.Error(ctx, err)
		}
	}()
	return nil
}
