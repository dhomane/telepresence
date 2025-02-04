package dos_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/datawire/dlib/dlog"
	"github.com/telepresenceio/telepresence/v2/pkg/dos"
	"github.com/telepresenceio/telepresence/v2/pkg/dos/aferofs"
)

func TestWithFS(t *testing.T) {
	appFS := afero.NewMemMapFs()
	cData := []byte("file c\n")
	dData := []byte("file d\n")
	require.NoError(t, appFS.MkdirAll("a/b", 0o755))
	require.NoError(t, afero.WriteFile(appFS, "/a/b/c.txt", cData, 0o644))
	require.NoError(t, afero.WriteFile(appFS, "/a/d.txt", dData, 0o644))

	ctx := dos.WithFS(dlog.NewTestContext(t, false), dos.WorkingDirWrapper(aferofs.Wrap(appFS)))

	require.NoError(t, dos.Chdir(ctx, "/a/b"))
	data, err := dos.ReadFile(ctx, "c.txt")
	require.NoError(t, err)
	require.Equal(t, cData, data)

	require.NoError(t, dos.Chdir(ctx, "../.."))
	f, err := dos.Open(ctx, "/a/d.txt")
	require.NoError(t, err)
	data, err = io.ReadAll(f)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.Equal(t, dData, data)
}

// Example using afero.MemMapFs.
func ExampleWithFS() {
	appFS := afero.NewCopyOnWriteFs(afero.NewOsFs(), afero.NewMemMapFs())
	ctx := dos.WithFS(context.Background(), aferofs.Wrap(appFS))

	if err := dos.MkdirAll(ctx, "/etc", 0o700); err != nil {
		log.Fatal(err)
	}
	hosts, err := dos.Create(ctx, "/etc/example.conf")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Fprintln(hosts, "example = conf")
	hosts.Close()

	if hosts, err = dos.Open(ctx, "/etc/example.conf"); err != nil {
		log.Fatal(err)
	}
	_, err = io.Copy(os.Stdout, hosts)
	_ = hosts.Close()
	if err != nil {
		log.Fatal(err)
	}

	if hosts, err = dos.Open(context.Background(), "/etc/example.conf"); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			fmt.Println("file does not exist")
		} else {
			fmt.Println(err)
		}
	}
	// Output:
	// example = conf
	// file does not exist
}
