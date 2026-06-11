package contracts

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/types"
	"sort"
	"strings"
)

// Fingerprint is a stable, content-addressed digest of a contract's
// public surface. Stage 8 (verification) compares two fingerprints
// across snapshots to detect tampering: a contract is considered
// stable when its fingerprint is unchanged.
//
// For an interface contract, the fingerprint hashes the sorted list
// of `<methodName>(<params>) <results>` strings. For a non-interface
// contract, it hashes the list of exported field signatures, which
// matches the tampering surface a Stage-8 linter cares about
// (renamed methods, changed parameter types).
//
// The format is intentionally narrow — internal types, comments, and
// AST positions don't participate. That keeps the fingerprint stable
// across formatting-only edits while still flipping when a method
// signature changes.
type Fingerprint struct {
	Identifier string   // `pkg/path.Name` for traceability
	Kind       string   // "interface" or "type"
	Members    []string // canonical, sorted member signatures
	Digest     string   // sha256 over the canonical lines, hex-encoded
}

// FingerprintOf computes the fingerprint for the resolved contract.
// Returns ok=false when the underlying type is neither an interface
// nor a struct (rare but possible for type aliases of basic kinds —
// fingerprinting those is a no-op).
func FingerprintOf(r Resolved) (Fingerprint, bool) {
	if r.Obj == nil {
		return Fingerprint{}, false
	}
	id := r.PkgPath + "." + r.TypeName
	switch under := r.Obj.Type().Underlying().(type) {
	case *types.Interface:
		members := interfaceMembers(under)
		return makeFingerprint(id, "interface", members), true
	case *types.Struct:
		members := structMembers(under)
		return makeFingerprint(id, "type", members), true
	}
	return Fingerprint{}, false
}

func makeFingerprint(id, kind string, members []string) Fingerprint {
	sort.Strings(members)
	hash := sha256.New()
	hash.Write([]byte(id))
	hash.Write([]byte("\x00"))
	hash.Write([]byte(kind))
	hash.Write([]byte("\x00"))
	for _, m := range members {
		hash.Write([]byte(m))
		hash.Write([]byte("\n"))
	}
	return Fingerprint{
		Identifier: id,
		Kind:       kind,
		Members:    members,
		Digest:     hex.EncodeToString(hash.Sum(nil)),
	}
}

// interfaceMembers returns "Method(p1Type, p2Type) (r1Type, r2Type)"
// strings for every explicit and embedded method in the interface.
func interfaceMembers(iface *types.Interface) []string {
	n := iface.NumMethods()
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		m := iface.Method(i)
		out = append(out, methodSignature(m))
	}
	return out
}

// structMembers returns "FieldName: Type" for every exported field.
// Unexported fields are excluded — the contract is the *external* shape
// (and our config-based contract surface declares whole types, not
// per-field overrides).
func structMembers(s *types.Struct) []string {
	out := make([]string, 0, s.NumFields())
	for i := 0; i < s.NumFields(); i++ {
		f := s.Field(i)
		if !f.Exported() {
			continue
		}
		out = append(out, fmt.Sprintf("%s: %s", f.Name(), f.Type().String()))
	}
	return out
}

func methodSignature(fn *types.Func) string {
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return fn.Name() + "()"
	}
	var b strings.Builder
	b.WriteString(fn.Name())
	b.WriteByte('(')
	writeTuple(&b, sig.Params())
	b.WriteByte(')')
	if sig.Results() != nil && sig.Results().Len() > 0 {
		b.WriteByte(' ')
		if sig.Results().Len() > 1 {
			b.WriteByte('(')
		}
		writeTuple(&b, sig.Results())
		if sig.Results().Len() > 1 {
			b.WriteByte(')')
		}
	}
	return b.String()
}

func writeTuple(b *strings.Builder, t *types.Tuple) {
	for i := 0; i < t.Len(); i++ {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(t.At(i).Type().String())
	}
}
