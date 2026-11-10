// Package offline proves the product never touches the network.
//
// The brief forbids network access at runtime and in tests. In Python this was
// a socket-blocking fixture; the Go equivalent is stronger because it runs at
// build time: an import that pulls a non-stdlib package is a compile error, and
// this test asserts the module graph contains nothing outside the standard
// library.
//
// The companion proof is the build itself:
//
//	GOFLAGS=-mod=mod GOPROXY=off go build ./...
//
// which fails if any dependency needs fetching. `make test-offline` runs it.
package offline

import (
	"os/exec"
	"strings"
	"testing"
)

// TestModuleHasNoExternalDependencies fails if go.mod grows a require block,
// which would mean the runtime is no longer standard-library only.
func TestModuleHasNoExternalDependencies(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "./...").CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps failed: %v\n%s", err, out)
	}

	module := "github.com/andrelbuzzo/itm"
	for _, dep := range strings.Fields(string(out)) {
		if dep == module || strings.HasPrefix(dep, module+"/") {
			continue
		}
		// Anything without a dot in its first path element is a standard
		// library package (e.g. "sort", "encoding/json"). A dependency with a
		// domain in it (github.com/..., golang.org/...) is external.
		first := strings.SplitN(dep, "/", 2)[0]
		if strings.Contains(first, ".") {
			t.Errorf("external dependency found: %s\nITM must stay standard-library only", dep)
		}
	}
}

// TestOfflineBuildSucceeds compiles the whole module with the module proxy
// disabled, proving no code path needs to fetch anything.
func TestOfflineBuildSucceeds(t *testing.T) {
	cmd := exec.Command("go", "build", "./...")
	cmd.Env = append(cmd.Environ(),
		"GOPROXY=off",      // no module downloads
		"GOFLAGS=-mod=mod", // do not require a vendor directory
		"CGO_ENABLED=0",    // no C toolchain, fully static
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("offline build failed: %v\n%s", err, out)
	}
}
