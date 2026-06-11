package graph

// Role-related Attrs keys and helpers. Roles are an architectural
// annotation orthogonal to the contract marker (ADR-009): the contract
// flag answers "is this part of a stable interface?", role answers
// "where does this live in the architecture?". Both axes coexist on
// the same Node via the Attrs map.
//
// See docs/decisions/027-role-metadata.md for the rationale.

// Role is a typed string for an architectural role. Allowed values are
// constrained at config-load time (see internal/roles).
type Role string

// Package-level roles. These describe layers in the architecture.
const (
	RolePackageDomain          Role = "domain"
	RolePackageApplication     Role = "application"
	RolePackageInboundAdapter  Role = "inbound_adapter"
	RolePackageOutboundAdapter Role = "outbound_adapter"
	RolePackageInfrastructure  Role = "infrastructure"
	RolePackageShared          Role = "shared"
)

// Type/symbol-level roles. These describe the kind of thing a node is
// from the architecture's point of view.
const (
	RoleTypeDomainEntity   Role = "domain_entity"
	RoleTypeValueObject    Role = "value_object"
	RoleTypePort           Role = "port"
	RoleTypeAdapterDTO     Role = "adapter_dto"
	RoleTypeConfigContract Role = "config_contract"
	RoleTypeExternalNoise  Role = "external_noise"
)

// AllPackageRoles returns the set of allowed package-scoped roles in
// stable order.
func AllPackageRoles() []Role {
	return []Role{
		RolePackageDomain,
		RolePackageApplication,
		RolePackageInboundAdapter,
		RolePackageOutboundAdapter,
		RolePackageInfrastructure,
		RolePackageShared,
	}
}

// AllTypeRoles returns the set of allowed type/symbol-scoped roles in
// stable order.
func AllTypeRoles() []Role {
	return []Role{
		RoleTypeDomainEntity,
		RoleTypeValueObject,
		RoleTypePort,
		RoleTypeAdapterDTO,
		RoleTypeConfigContract,
		RoleTypeExternalNoise,
	}
}

// Attrs keys for role metadata.
const (
	// AttrRole is the Attrs key holding the resolved Role string.
	AttrRole = "role"
	// AttrRoleSource records the provenance of the role marker:
	// "type" (explicit type/symbol selector match), "package"
	// (explicit package selector match), or "inferred" (future use).
	AttrRoleSource = "roleSource"
)

// Role returns the architectural role assigned to the node, or "" if
// none is set.
func (n Node) Role() Role {
	if n.Attrs == nil {
		return ""
	}
	v, ok := n.Attrs[AttrRole]
	if !ok {
		return ""
	}
	switch s := v.(type) {
	case Role:
		return s
	case string:
		return Role(s)
	default:
		return ""
	}
}

// RoleSource returns the provenance of the role marker ("type",
// "package", "inferred") or "" when no role is set.
func (n Node) RoleSource() string {
	if n.Attrs == nil {
		return ""
	}
	v, ok := n.Attrs[AttrRoleSource]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// SetRole assigns the role and source to the node with the given stable
// ID. Returns true on success, false when the node is unknown. Callers
// in internal/roles use this through a single resolver pass; consumers
// outside that package should treat roles as read-only.
func (g *Graph) SetRole(id string, role Role, source string) bool {
	entry, ok := g.nodes[id]
	if !ok {
		return false
	}
	if entry.node.Attrs == nil {
		entry.node.Attrs = make(map[string]any)
	}
	entry.node.Attrs[AttrRole] = string(role)
	if source != "" {
		entry.node.Attrs[AttrRoleSource] = source
	}
	return true
}
