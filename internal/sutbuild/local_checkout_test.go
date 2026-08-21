package sutbuild

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestConsensusModulesResolveToPinnedLocalCheckouts(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		module  string
		version string
		dir     string
	}{
		{module: "go.etcd.io/raft/v3", version: "v3.6.0", dir: "suts/etcdraft"},
		{module: "github.com/hashicorp/raft", version: "v1.7.3", dir: "suts/hashicorpraft"},
	} {
		t.Run(test.module, func(t *testing.T) {
			command := exec.Command("go", "list", "-mod=readonly", "-m", "-json", test.module)
			command.Dir = repoRoot
			command.Env = controlledEnvironment(command.Environ())
			output, err := command.Output()
			if err != nil {
				t.Fatal(err)
			}
			var resolved moduleInfo
			if err := json.Unmarshal(output, &resolved); err != nil {
				t.Fatal(err)
			}
			wantDir, err := filepath.EvalSymlinks(filepath.Join(repoRoot, test.dir))
			if err != nil {
				t.Fatal(err)
			}
			gotDir, err := filepath.EvalSymlinks(resolved.Dir)
			if err != nil {
				t.Fatal(err)
			}
			if resolved.Path != test.module || resolved.Version != test.version || gotDir != wantDir {
				t.Fatalf("resolved %s@%s in %s; want %s@%s in %s",
					resolved.Path, resolved.Version, gotDir, test.module, test.version, wantDir)
			}
		})
	}
}
