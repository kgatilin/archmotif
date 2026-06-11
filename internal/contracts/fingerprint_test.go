package contracts

import (
	"testing"
)

// TestFingerprint_Stability is the Stage-2 groundwork for Stage-8
// tampering detection. The fingerprint of a contract must be:
//
//   - reproducible across two runs of Build on the same fixture, and
//   - distinct from a fingerprint computed when one of the methods is
//     hand-edited.
//
// Stage 2 doesn't implement detection — Stage 8 will compare two
// snapshots. This test asserts the snapshot mechanism exists and is
// deterministic.
func TestFingerprint_Stability(t *testing.T) {
	dir := fixtureDir(t)
	a, err := Build(BuildOptions{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Build(BuildOptions{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}

	fps := func(r *Result) map[string]Fingerprint {
		out := make(map[string]Fingerprint, len(r.Resolved))
		for _, x := range r.Resolved {
			fp, ok := FingerprintOf(x)
			if !ok {
				continue
			}
			out[fp.Identifier] = fp
		}
		return out
	}
	aFp := fps(a)
	bFp := fps(b)
	if len(aFp) == 0 {
		t.Fatal("expected at least one fingerprint")
	}
	for id, fa := range aFp {
		fb, ok := bFp[id]
		if !ok {
			t.Fatalf("contract %q present in run A but missing in run B", id)
		}
		if fa.Digest != fb.Digest {
			t.Fatalf("contract %q digest changed across identical runs:\n A=%s\n B=%s",
				id, fa.Digest, fb.Digest)
		}
	}
}

// TestFingerprint_DigestSensitivity asserts the fingerprint
// distinguishes a known-good interface from a hand-mutated copy with a
// renamed method (the canonical Stage-8 tampering signal).
func TestFingerprint_DigestSensitivity(t *testing.T) {
	good := makeFingerprint("pkg.Iface", "interface", []string{
		"Find(string) (User, error)",
		"Save(User) error",
	})
	tampered := makeFingerprint("pkg.Iface", "interface", []string{
		"FindUser(string) (User, error)", // method renamed
		"Save(User) error",
	})
	if good.Digest == tampered.Digest {
		t.Fatalf("digests should differ when a method is renamed:\n good=%s\n tampered=%s",
			good.Digest, tampered.Digest)
	}

	// And member ordering must not affect the digest — Stage 8 compares
	// two parses of the same source, and the order of methods in a
	// types.Interface is not guaranteed.
	reordered := makeFingerprint("pkg.Iface", "interface", []string{
		"Save(User) error",
		"Find(string) (User, error)",
	})
	if good.Digest != reordered.Digest {
		t.Fatalf("member-order change must not flip digest:\n good=%s\n reordered=%s",
			good.Digest, reordered.Digest)
	}
}
