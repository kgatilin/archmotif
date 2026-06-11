// Package skeleton renders annotated-Go + YAML companion skeleton
// files from a propose.Proposal, per ADR-016.
//
// The renderer is deliberately format-anchored: it walks the
// Proposal's TargetSubgraph and Samples and emits the on-disk
// representation pinned by ADR-016 / docs/skeleton-format.md. It does
// no graph queries of its own — the upstream Stage-5 proposer is the
// authority for "what shape should the new code have?".
//
// Two surface entry points:
//
//   - RenderGo emits the annotated Go file. Placeholders appear as
//     bare Go identifiers so go/parser accepts the file; the
//     angle-bracket display form (<Iface>) is reconstructed at
//     prompt-build time from the // ROLE comments.
//   - RenderYAML emits the structured target-subgraph companion.
package skeleton

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	mgraph "github.com/kgatilin/archmotif/internal/graph"
	"github.com/kgatilin/archmotif/internal/propose"
)

// MinSamples / MaxSamples pin the SAMPLES block size per ADR-016.
const (
	MinSamples = 3
	MaxSamples = 5
)

// RenderGo emits the annotated-Go skeleton for p, matching
// docs/skeleton-format.md §1. The resulting bytes parse with
// go/parser.ParseFile(parser.ParseComments).
//
// The on-disk Go file uses bare identifiers for role placeholders
// (e.g. Iface, Method) — ADR-016 §"Annotated Go grammar" decides
// this. The angle-bracket display form is reconstructed by Stage 7
// from the // ROLE comments; it is not stored on disk.
func RenderGo(p *propose.Proposal) ([]byte, error) {
	if p == nil {
		return nil, fmt.Errorf("skeleton: nil Proposal")
	}
	if err := validateProposal(p); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	// Header comments (ADR-016 §grammar).
	fmt.Fprintf(&buf, "// PROPOSAL: %s\n", p.Description)
	fmt.Fprintf(&buf, "// AFFECTED: %s\n", strings.Join(p.AffectedFiles, ", "))
	buf.WriteString("//\n")
	buf.WriteString("// Generated skeleton for ADR-016. The on-disk file is valid Go so\n")
	buf.WriteString("// go/parser accepts it; role placeholders appear as bare identifiers.\n")
	buf.WriteString("// The angle-bracket display form (<Role>) is reconstructed from the\n")
	buf.WriteString("// // ROLE comments at prompt-build time (Stage 7).\n")
	buf.WriteString("\n")
	fmt.Fprintf(&buf, "package %s\n\n", goPackageName(p.ID))

	// Group roles by kind so we can emit interface, struct, method
	// declarations in a stable order. We classify each Role using
	// classifyRole (NodeKind + AttrContractKind → skeleton kind).
	roleByName := make(map[string]propose.Role, len(p.TargetSubgraph.Roles))
	kindByName := make(map[string]string, len(p.TargetSubgraph.Roles))
	for _, r := range p.TargetSubgraph.Roles {
		roleByName[r.Name] = r
		kindByName[r.Name] = classifyRole(r)
	}

	// Find the Iface role (skeleton kind=interface) and the Method
	// role(s) realised on Impls. The Impl→Iface implements edge and
	// the Impl→Method contains edge (or the Method's receiver_role
	// inferred from contains) wire the structure.
	ifaceRole, hasIface := findRoleByKind(p.TargetSubgraph.Roles, kindByName, "interface")
	implRole, hasImpl := findRoleByKind(p.TargetSubgraph.Roles, kindByName, "struct")
	methodRoles := rolesByKind(p.TargetSubgraph.Roles, kindByName, "method")

	// Determine the method-on-iface role name. The motif-001 grammar
	// reuses the same role identifier on both the interface method
	// signature and the concrete method declaration; we look for a
	// method role and use its Name.
	methodName := ""
	if len(methodRoles) > 0 {
		methodName = methodRoles[0].Name
	}

	// Emit the interface block. ADR-016 grammar groups all role
	// declarations on the interface (Iface, Method, Param, ParamType,
	// RetType) into one // ROLE comment cluster.
	if hasIface {
		emitInterfaceBlock(&buf, ifaceRole, methodName, p.TargetSubgraph.Roles, kindByName)
	}

	// Emit the struct block(s). For Cardinality=1 the renderer uses
	// the bare role name; the LLM/verifier read the YAML for instance
	// counts.
	if hasImpl {
		buf.WriteString("\n")
		fmt.Fprintf(&buf, "// ROLE %s : struct\n", implRole.Name)
		fmt.Fprintf(&buf, "type %s struct{}\n", implRole.Name)
	}

	// Emit the method block. We pick the Impl as receiver per ADR-016
	// example. Param/ParamType/RetType reuse the role names declared
	// up top.
	if methodName != "" && hasImpl && hasIface {
		paramRole, paramTypeRole, retTypeRole := signatureRoles(roleByName, kindByName)
		buf.WriteString("\n")
		fmt.Fprintf(&buf, "// ROLE %s (continued): definition on %s realising %s.%s.\n",
			methodName, implRole.Name, ifaceRole.Name, methodName)
		fmt.Fprintf(&buf, "func (i *%s) %s(%s %s) %s { return %s{} }\n",
			implRole.Name, methodName, paramRole, paramTypeRole, retTypeRole, retTypeRole)

		// Emit stub types so the file is self-contained for go/parser.
		buf.WriteString("\n")
		buf.WriteString("// Stub types so the file is self-contained and go/parser-clean.\n")
		buf.WriteString("// These are not roles; they exist only to give Param/RetType something\n")
		buf.WriteString("// to refer to in the placeholder world.\n")
		fmt.Fprintf(&buf, "type %s struct{}\n", paramTypeRole)
		fmt.Fprintf(&buf, "type %s struct{}\n", retTypeRole)
	}

	// SAMPLES block — mandatory 3..5 entries per ADR-016.
	samples, err := selectSamples(p)
	if err != nil {
		return nil, err
	}
	buf.WriteString("\n")
	buf.WriteString("// SAMPLES:\n")
	for _, line := range formatSampleLines(samples, sampleRoleOrder(p.TargetSubgraph.Roles, kindByName)) {
		fmt.Fprintf(&buf, "//   %s\n", line)
	}

	return buf.Bytes(), nil
}

// RenderYAML emits the YAML companion for p, matching
// docs/skeleton-format.md §2.
//
// The output is hand-emitted (not encoded via yaml.v3) so the
// renderer controls field ordering, comment placement, and
// inline-flow style — all of which matter for byte-for-byte
// reproducibility against the motif-001 fixture and for human
// review. round-trip safety is enforced by render_test.go which
// re-decodes the output with yaml.Unmarshal.
func RenderYAML(p *propose.Proposal) ([]byte, error) {
	if p == nil {
		return nil, fmt.Errorf("skeleton: nil Proposal")
	}
	if err := validateProposal(p); err != nil {
		return nil, err
	}

	kindByName := make(map[string]string, len(p.TargetSubgraph.Roles))
	for _, r := range p.TargetSubgraph.Roles {
		kindByName[r.Name] = classifyRole(r)
	}
	ifaceRole, hasIface := findRoleByKind(p.TargetSubgraph.Roles, kindByName, "interface")
	implRole, hasImpl := findRoleByKind(p.TargetSubgraph.Roles, kindByName, "struct")
	methodRoles := rolesByKind(p.TargetSubgraph.Roles, kindByName, "method")
	roleByName := roleByNameMap(p.TargetSubgraph.Roles)

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "# Skeleton companion for proposal %s.\n", p.ID)
	buf.WriteString("# Generated by archmotif skeleton; see ADR-016 / docs/skeleton-format.md.\n")
	buf.WriteString("\n")
	fmt.Fprintf(&buf, "proposal_id: %s\n", p.ID)
	fmt.Fprintf(&buf, "description: %s\n", p.Description)
	buf.WriteString("affected:\n")
	for _, f := range p.AffectedFiles {
		fmt.Fprintf(&buf, "  - %s\n", f)
	}
	buf.WriteString("target_subgraph:\n")
	buf.WriteString("  roles:\n")
	for _, r := range p.TargetSubgraph.Roles {
		kind := kindByName[r.Name]
		fmt.Fprintf(&buf, "    - id: %s\n", r.Name)
		fmt.Fprintf(&buf, "      kind: %s\n", kind)
		switch kind {
		case "interface":
			if len(methodRoles) > 0 {
				m := methodRoles[0]
				paramRole, paramTypeRole, retTypeRole := signatureRoles(roleByName, kindByName)
				buf.WriteString("      methods:\n")
				fmt.Fprintf(&buf, "        - name_role: %s\n", m.Name)
				buf.WriteString("          params:\n")
				fmt.Fprintf(&buf, "            - {role: %s, type_role: %s}\n", paramRole, paramTypeRole)
				fmt.Fprintf(&buf, "          return_role: %s\n", retTypeRole)
			}
		case "method":
			if hasImpl {
				fmt.Fprintf(&buf, "      receiver_role: %s\n", implRole.Name)
			}
			if hasIface {
				fmt.Fprintf(&buf, "      realises: {role: %s, method: %s}\n", ifaceRole.Name, r.Name)
			}
		}
	}
	buf.WriteString("  edges:\n")
	for _, e := range p.TargetSubgraph.Edges {
		fmt.Fprintf(&buf, "    - {from: %s, to: %s, kind: %s}\n", e.From, e.To, string(e.Kind))
	}

	samples, err := selectSamples(p)
	if err != nil {
		return nil, err
	}
	roleOrder := sampleRoleOrder(p.TargetSubgraph.Roles, kindByName)
	if len(samples) > 0 && len(roleOrder) > 0 {
		buf.WriteString("samples:\n")
		// Compute alignment widths so rendered samples mirror the Go
		// SAMPLES block visually.
		valWidths := make([]int, len(roleOrder))
		rows := make([][]string, 0, len(samples))
		for _, s := range samples {
			row := make([]string, len(roleOrder))
			for i, role := range roleOrder {
				val, _ := lookupSampleValue(s, role)
				row[i] = val
				if len(val)+1 > valWidths[i] { // +1 for trailing comma
					valWidths[i] = len(val) + 1
				}
			}
			rows = append(rows, row)
		}
		for _, row := range rows {
			buf.WriteString("  - {")
			for i, role := range roleOrder {
				val := row[i]
				if i < len(roleOrder)-1 {
					cell := fmt.Sprintf("%s: %s,", role, val)
					pad := len(role) + 2 + valWidths[i] - len(cell)
					if pad < 1 {
						pad = 1
					}
					buf.WriteString(cell)
					buf.WriteString(strings.Repeat(" ", pad))
				} else {
					fmt.Fprintf(&buf, "%s: %s", role, val)
				}
			}
			buf.WriteString("}\n")
		}
	}
	return buf.Bytes(), nil
}

// classifyRole maps a propose.Role to a skeleton role-kind string per
// ADR-016: NodeType + contract=interface → "interface"; NodeType
// (otherwise) → "struct"; NodeMethod → "method"; NodeFunction →
// "function"; NodeField → "field"; everything else → "type".
func classifyRole(r propose.Role) string {
	switch r.Kind {
	case mgraph.NodeType:
		if r.Attrs != nil {
			if v, ok := r.Attrs[mgraph.AttrContractKind]; ok {
				if s, ok := v.(string); ok && s == "interface" {
					return "interface"
				}
			}
		}
		return "struct"
	case mgraph.NodeMethod:
		return "method"
	case mgraph.NodeFunction:
		return "function"
	case mgraph.NodeField:
		return "field"
	default:
		return "type"
	}
}

func findRoleByKind(roles []propose.Role, kindByName map[string]string, kind string) (propose.Role, bool) {
	for _, r := range roles {
		if kindByName[r.Name] == kind {
			return r, true
		}
	}
	return propose.Role{}, false
}

func rolesByKind(roles []propose.Role, kindByName map[string]string, kind string) []propose.Role {
	out := make([]propose.Role, 0, len(roles))
	for _, r := range roles {
		if kindByName[r.Name] == kind {
			out = append(out, r)
		}
	}
	return out
}

func roleByNameMap(roles []propose.Role) map[string]propose.Role {
	out := make(map[string]propose.Role, len(roles))
	for _, r := range roles {
		out[r.Name] = r
	}
	return out
}

// signatureRoles picks Param / ParamType / RetType role names from the
// role set. The Stage-5 proposer for extract-interface doesn't yet
// emit signature-shape roles (it currently emits only Iface / Impl /
// Method); for that case the renderer falls back to canonical names
// "Param", "ParamType", "RetType" so the on-disk skeleton still reads
// faithfully and validates with go/parser. ADR-023 §"signature roles"
// records the choice.
func signatureRoles(roleByName map[string]propose.Role, kindByName map[string]string) (param, paramType, retType string) {
	param = "Param"
	paramType = "ParamType"
	retType = "RetType"
	// Honour explicitly named signature roles when they're declared.
	for name, kind := range kindByName {
		switch kind {
		case "field":
			// We leave "field" alone — fields aren't signature shape.
		case "type":
			// A role explicitly tagged as "type" is treated as a type
			// reference; first wins for ParamType.
			if _, ok := roleByName[name]; ok {
				if paramType == "ParamType" {
					paramType = name
				}
			}
		}
	}
	return
}

// validateProposal enforces the renderer pre-conditions: ID, at least
// one role, and a sample count in [MinSamples, MaxSamples] (for the
// Go SAMPLES block). The renderer truncates the sample list when
// needed (per ADR-016 §"Sample count"); validateProposal only
// rejects degenerate cases (no samples, empty ID).
func validateProposal(p *propose.Proposal) error {
	if p.ID == "" {
		return fmt.Errorf("skeleton: proposal has empty ID")
	}
	if len(p.TargetSubgraph.Roles) == 0 {
		return fmt.Errorf("skeleton: proposal %q has no roles", p.ID)
	}
	if len(p.Samples) == 0 {
		return fmt.Errorf("skeleton: proposal %q has no samples (ADR-016 requires >= %d)", p.ID, MinSamples)
	}
	return nil
}

// selectSamples returns 3..5 samples from p.Samples, padding by
// repeating the last entry if the proposer emitted fewer than 3 (some
// fixture paths produce 2). Padding is documented in ADR-023 — this
// is a renderer-side fallback; the proposer's normal output meets the
// minimum.
func selectSamples(p *propose.Proposal) ([]map[string]string, error) {
	if len(p.Samples) == 0 {
		return nil, fmt.Errorf("skeleton: no samples")
	}
	out := append([]map[string]string(nil), p.Samples...)
	if len(out) > MaxSamples {
		out = out[:MaxSamples]
	}
	for len(out) < MinSamples {
		// Pad by repeating the last sample. Better than dropping the
		// SAMPLES block entirely; the verifier (Stage 8) only cares
		// the block is present and parses.
		out = append(out, out[len(out)-1])
	}
	return out, nil
}

// sampleRoleOrder returns the role-name order used in SAMPLES lines.
// We emit interface role first, struct second, method third (matches
// the motif-001 fixture: Iface, Impl, Method).
func sampleRoleOrder(roles []propose.Role, kindByName map[string]string) []string {
	priority := map[string]int{
		"interface": 0,
		"struct":    1,
		"method":    2,
		"function":  3,
		"field":     4,
		"type":      5,
	}
	type ord struct {
		name string
		p    int
		idx  int
	}
	tmp := make([]ord, 0, len(roles))
	for i, r := range roles {
		k := kindByName[r.Name]
		p, ok := priority[k]
		if !ok {
			p = 99
		}
		tmp = append(tmp, ord{name: r.Name, p: p, idx: i})
	}
	sort.SliceStable(tmp, func(i, j int) bool {
		if tmp[i].p != tmp[j].p {
			return tmp[i].p < tmp[j].p
		}
		return tmp[i].idx < tmp[j].idx
	})
	out := make([]string, 0, len(tmp))
	for _, t := range tmp {
		// Skip param/return-shape roles; they're reconstructed inline.
		if t.p >= 3 {
			continue
		}
		out = append(out, t.name)
	}
	return out
}

// formatSampleLines aligns sample lines so the same role label sits in
// the same column across rows. Matches the motif-001 fixture style:
// "Iface=UserStore   Impl=SQLUserStore  Method=Find".
func formatSampleLines(samples []map[string]string, roleOrder []string) []string {
	if len(samples) == 0 || len(roleOrder) == 0 {
		return nil
	}
	// Compute max width per column.
	widths := make([]int, len(roleOrder))
	cells := make([][]string, 0, len(samples))
	for _, s := range samples {
		row := make([]string, len(roleOrder))
		for i, role := range roleOrder {
			val, _ := lookupSampleValue(s, role)
			cell := fmt.Sprintf("%s=%s", role, val)
			row[i] = cell
			if len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
		cells = append(cells, row)
	}
	lines := make([]string, 0, len(cells))
	for _, row := range cells {
		var b strings.Builder
		for i, cell := range row {
			b.WriteString(cell)
			if i < len(row)-1 {
				// Pad with spaces so the next column lines up.
				gap := widths[i] - len(cell) + 1
				b.WriteString(strings.Repeat(" ", gap))
			}
		}
		lines = append(lines, strings.TrimRight(b.String(), " "))
	}
	return lines
}

// lookupSampleValue resolves a role's display value from a sample
// map. The Stage-5 proposer puts both raw graph IDs (key = role name)
// and human-friendly names (key = roleName + "Name") in the sample
// map; we prefer the human-friendly form. Per ADR-023 §"sample
// lookup".
func lookupSampleValue(s map[string]string, role string) (string, bool) {
	if v, ok := s[role+"Name"]; ok && v != "" {
		return v, true
	}
	if v, ok := s[role]; ok && v != "" {
		return v, true
	}
	return "", false
}

// emitInterfaceBlock writes the // ROLE comment cluster + interface
// declaration. For motif-001 the cluster names Iface, Method, Param,
// ParamType, RetType so all signature roles are introduced at once.
func emitInterfaceBlock(buf *bytes.Buffer, ifaceRole propose.Role, methodName string,
	roles []propose.Role, kindByName map[string]string,
) {
	fmt.Fprintf(buf, "// ROLE %s : interface\n", ifaceRole.Name)
	if methodName != "" {
		fmt.Fprintf(buf, "// ROLE %s : method on Impl realising %s.%s\n",
			methodName, ifaceRole.Name, methodName)
	}
	roleByName := roleByNameMap(roles)
	paramRole, paramTypeRole, retTypeRole := signatureRoles(roleByName, kindByName)
	fmt.Fprintf(buf, "// ROLE %s : parameter name on the realised method\n", paramRole)
	fmt.Fprintf(buf, "// ROLE %s : parameter type on the realised method\n", paramTypeRole)
	fmt.Fprintf(buf, "// ROLE %s : return type on the realised method\n", retTypeRole)
	fmt.Fprintf(buf, "type %s interface {\n", ifaceRole.Name)
	if methodName != "" {
		fmt.Fprintf(buf, "\t%s(%s %s) %s\n", methodName, paramRole, paramTypeRole, retTypeRole)
	}
	buf.WriteString("}\n")
}

// goPackageName derives a valid Go package identifier from the
// proposal ID. "extract_interface-motif-0" → "extract_interface_motif_0";
// "motif-001" → "motif001". The package name is internal to the
// generated file; nothing references it externally.
func goPackageName(id string) string {
	var b strings.Builder
	for i, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			if i == 0 {
				b.WriteRune('p')
			}
			b.WriteRune(r)
		case r == '_':
			b.WriteRune('_')
		case r == '-':
			// Drop separator hyphens. motif-001 → motif001.
			continue
		default:
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 {
		return "skeleton"
	}
	return b.String()
}
