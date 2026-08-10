package admin

// Unit tests for getFreeDiskSpace's dev fallback: when the exact storage path
// cannot exist/be created (the default /app/storage is Docker-only and a bare
// dev machine cannot mkdir /app as non-root), the helper must fall back to the
// nearest existing ancestor so dev UIs get a realistic free-disk value instead
// of 0 (which would silently disable every disk cap).

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetFreeDiskSpaceRealDir(t *testing.T) {
	dir := t.TempDir()
	free := getFreeDiskSpace(dir)
	if free <= 0 {
		t.Fatalf("getFreeDiskSpace(%q) = %v, want > 0 (real writable dir)", dir, free)
	}
}

func TestGetFreeDiskSpaceAncestorFallback(t *testing.T) {
	// A path deep under a non-existent root (mirrors /app/storage on a dev
	// machine): statfs on it must fail, and the helper must climb up to an
	// existing ancestor instead of returning 0.
	missing := filepath.Join(t.TempDir(), "does", "not", "exist", "storage")
	free := getFreeDiskSpace(missing)
	if free <= 0 {
		t.Fatalf("getFreeDiskSpace(%q) = %v, want > 0 via ancestor fallback", missing, free)
	}

	// The fallback value must agree with a statfs directly on the ancestor
	// (same partition), so the returned number is the REAL free disk, not a
	// made-up constant.
	root := filepath.VolumeName(missing) + string(filepath.Separator)
	if _, err := os.Stat(root); err != nil {
		t.Skipf("no statfs-able root %q: %v", root, err)
	}
	want := getFreeDiskSpace(root)
	if diff := free - want; diff < 0 || diff > want {
		t.Fatalf("fallback %v disagrees with root statfs %v (diff %v)", free, want, diff)
	}
}

func TestGetFreeDiskSpaceNeverZeroOnDevLikePath(t *testing.T) {
	// The exact historical dev failure: the compiled-in default path.
	free := getFreeDiskSpace("/app/storage")
	if free <= 0 {
		t.Fatalf("getFreeDiskSpace(/app/storage) = %v, want > 0 (ancestor fallback)", free)
	}
}
