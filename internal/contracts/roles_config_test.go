package contracts

import (
	"strings"
	"testing"
)

func TestReadConfig_Roles_OK(t *testing.T) {
	src := `roles:
  packages:
    - pattern: "internal/domain/**"
      role: domain
    - pattern: "internal/adapters/**/inbound/*"
      role: inbound_adapter
  types:
    - qualified: "internal/domain.User"
      role: domain_entity
    - pattern: "*Request"
      role: adapter_dto
`
	cfg, err := readConfig(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if got := len(cfg.Roles.Packages); got != 2 {
		t.Fatalf("packages = %d, want 2", got)
	}
	if cfg.Roles.Packages[0].Pattern != "internal/domain/**" {
		t.Fatalf("packages[0].Pattern = %q", cfg.Roles.Packages[0].Pattern)
	}
	if cfg.Roles.Packages[0].Role != "domain" {
		t.Fatalf("packages[0].Role = %q", cfg.Roles.Packages[0].Role)
	}
	if got := len(cfg.Roles.Types); got != 2 {
		t.Fatalf("types = %d, want 2", got)
	}
	if cfg.Roles.Types[0].Qualified != "internal/domain.User" {
		t.Fatalf("types[0].Qualified = %q", cfg.Roles.Types[0].Qualified)
	}
}

func TestReadConfig_Roles_RejectsBothSelectors(t *testing.T) {
	src := `roles:
  packages:
    - pattern: "x/**"
      qualified: "pkg.Y"
      role: domain
`
	if _, err := readConfig(strings.NewReader(src)); err == nil {
		t.Fatal("expected error when selector has both pattern: and qualified:")
	}
}

func TestReadConfig_Roles_RejectsNeitherSelector(t *testing.T) {
	src := `roles:
  packages:
    - role: domain
`
	if _, err := readConfig(strings.NewReader(src)); err == nil {
		t.Fatal("expected error when selector has neither pattern: nor qualified:")
	}
}

func TestReadConfig_Roles_RejectsUnknownPackageRole(t *testing.T) {
	src := `roles:
  packages:
    - pattern: "x/**"
      role: not_a_real_role
`
	if _, err := readConfig(strings.NewReader(src)); err == nil {
		t.Fatal("expected error for unknown package role")
	}
}

func TestReadConfig_Roles_RejectsTypeRoleInPackagesBlock(t *testing.T) {
	// `domain_entity` is a type-role; should not be allowed under packages.
	src := `roles:
  packages:
    - pattern: "x/**"
      role: domain_entity
`
	if _, err := readConfig(strings.NewReader(src)); err == nil {
		t.Fatal("expected error: type-role used in packages block")
	}
}

func TestReadConfig_Roles_RejectsPackageRoleInTypesBlock(t *testing.T) {
	src := `roles:
  types:
    - pattern: "*Foo"
      role: domain
`
	if _, err := readConfig(strings.NewReader(src)); err == nil {
		t.Fatal("expected error: package-role used in types block")
	}
}

func TestReadConfig_Roles_EmptyConfigOK(t *testing.T) {
	src := `contracts: []
`
	cfg, err := readConfig(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Roles.Packages) != 0 || len(cfg.Roles.Types) != 0 {
		t.Fatalf("empty config should have empty roles")
	}
}
