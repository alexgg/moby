package storagemigration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/docker/docker/pkg/archive"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
	"gotest.tools/assert"
	"gotest.tools/fs"
)

func setup(t *testing.T) (*fs.Dir, *State) {
	if testing.Verbose() {
		logrus.SetLevel(logrus.DebugLevel)
	}

	root := fs.NewDir(t, t.Name(),
		fs.WithDir("aufs",
			fs.WithDir("layers",
				fs.WithFile("b38c03118c1e41289cf0972f11453c9b", "",
					fs.WithMode(0755)),
				fs.WithFile("b8936bbae21948ed826207ced6fa19c5", "b38c03118c1e41289cf0972f11453c9b",
					fs.WithMode(0666)),
			),
			fs.WithDir("diff",
				fs.WithDir("b38c03118c1e41289cf0972f11453c9b",
					fs.WithFile("test", "")),
				fs.WithDir("b8936bbae21948ed826207ced6fa19c5",
					fs.WithFile(archive.WhiteoutPrefix+"test", "")),
			),
		),
		fs.WithDir("containers",
			fs.WithDir("bebe92422caf828ab21ae39974a0c003a29970ec09c6e5529bbb24f71eb9ca2ef",
				fs.WithFile("config.v2.json", `{"Driver": "aufs"}`),
				fs.WithFile("hostconfig.json", `{}`),
				fs.WithDir("checkpoints"),
			),
		),
	)
	state := &State{
		Layers: []Layer{
			{
				ID:        "b38c03118c1e41289cf0972f11453c9b",
				ParentIDs: nil,
				Meta:      nil,
			},
			{
				ID:        "b8936bbae21948ed826207ced6fa19c5",
				ParentIDs: []string{"b38c03118c1e41289cf0972f11453c9b"},
				Meta: []Meta{
					{Type: MetaWhiteout, Path: "test"},
				},
			},
		},
	}
	return root, state
}

func TestCreateState(t *testing.T) {
	root, expect := setup(t)
	defer root.Remove()

	state, err := createState(root.Join("aufs"))
	assert.NilError(t, err)
	assert.DeepEqual(t, state, expect)
}

func TestMigrate(t *testing.T) {
	root, _ := setup(t)
	defer root.Remove()

	// create a socket
	sockpath := root.Join("aufs/diff/b38c03118c1e41289cf0972f11453c9b/socket")
	assert.NilError(t, os.MkdirAll(filepath.Dir(sockpath), 0666))
	assert.NilError(t, unix.Mknod(sockpath, 0755|unix.S_IFSOCK, 0))

	err := Migrate(root.Path())
	assert.NilError(t, err)

	// overlay2 directory should exists
	_, err = os.Stat(root.Join("overlay2"))
	assert.NilError(t, err)
}

func TestFailCleanup(t *testing.T) {
	root, _ := setup(t)
	defer root.Remove()

	// delete diff directory to force createState to fail
	os.RemoveAll(root.Join("aufs", "diff"))

	logPath := root.Join("migrate.log")
	os.Setenv("BALENA_MIGRATE_OVERLAY_LOGFILE", logPath)
	defer func() {
		os.Unsetenv("BALENA_MIGRATE_OVERLAY_LOGFILE")
	}()

	err := Migrate(root.Path())
	assert.ErrorContains(t, err, "Error loading layer ids")

	// overlay2 directory should still exists
	_, err = os.Stat(root.Join("overlay2"))
	assert.ErrorType(t, err, os.IsNotExist)

	// aufs directory should still exists
	_, err = os.Stat(root.Join("aufs"))
	assert.NilError(t, err)

	// logfile should exists
	_, err = os.Stat(logPath)
	assert.NilError(t, err)
}

func TestCommit(t *testing.T) {
	root, _ := setup(t)
	defer root.Remove()

	err := Migrate(root.Path())
	assert.NilError(t, err)

	// overlay2 directory should still exists
	_, err = os.Stat(root.Join("overlay2"))
	assert.NilError(t, err)

	// aufs directory should still exists
	_, err = os.Stat(root.Join("aufs"))
	assert.NilError(t, err)

	// call again to trigger commit
	err = Migrate(root.Path())
	assert.NilError(t, err)

	// aufs directory should be cleaned up
	_, err = os.Stat(root.Join("aufs"))
	assert.ErrorType(t, err, os.IsNotExist)
}
