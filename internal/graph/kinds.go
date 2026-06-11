// Package graph defines the typed code graph used by archmotif.
//
// The graph is the central data structure for Stage 1: nodes are program
// elements (packages, files, types, functions, methods, fields, control-flow
// primitives), edges are typed relationships between them. The shape is
// intentionally close to "level 3.5" in the abstraction ladder
// (see docs/concepts.md): typed call graph plus selective control-flow
// primitives as their own nodes.
//
// gonum.org/v1/gonum/graph/simple backs the structural store; this package
// owns the typed payload (NodeKind, EdgeKind, attributes) so callers don't
// touch gonum directly.
package graph

// NodeKind is the type tag for a graph node.
type NodeKind string

// Node kinds. Keep the lowercase string forms stable — they participate in
// node IDs (see ADR-005) and JSON output.
const (
	NodePackage   NodeKind = "package"
	NodeFile      NodeKind = "file"
	NodeType      NodeKind = "type"
	NodeFunction  NodeKind = "function"
	NodeMethod    NodeKind = "method"
	NodeField     NodeKind = "field"
	NodeLoop      NodeKind = "loop"
	NodeBranch    NodeKind = "branch"
	NodeGoroutine NodeKind = "goroutine"
	NodeDefer     NodeKind = "defer"
	NodeChannelOp NodeKind = "channelop"
	NodeSyncPrim  NodeKind = "syncprim"
)

// AllNodeKinds returns every node kind archmotif recognises in Stage 1.
// Order is stable for deterministic enumeration.
func AllNodeKinds() []NodeKind {
	return []NodeKind{
		NodePackage,
		NodeFile,
		NodeType,
		NodeFunction,
		NodeMethod,
		NodeField,
		NodeLoop,
		NodeBranch,
		NodeGoroutine,
		NodeDefer,
		NodeChannelOp,
		NodeSyncPrim,
	}
}

// EdgeKind is the type tag for a graph edge.
type EdgeKind string

// Edge kinds.
const (
	// EdgeContains nests structural elements: package contains files,
	// file contains decls, function contains control-flow primitives, etc.
	EdgeContains EdgeKind = "contains"
	// EdgeImplements links a concrete type to an interface it satisfies.
	EdgeImplements EdgeKind = "implements"
	// EdgeEmbeds links a struct/interface to a type it embeds.
	EdgeEmbeds EdgeKind = "embeds"
	// EdgeCalls links the enclosing function/method to the callee.
	EdgeCalls EdgeKind = "calls"
	// EdgeCallsFrom links a control-flow primitive (loop/branch/goroutine/
	// defer) directly to the callee that fires inside it.
	EdgeCallsFrom EdgeKind = "callsFrom"
	// EdgeReferences links a function/method to a function or method used as
	// a value, for example a callback registered in a route table.
	EdgeReferences EdgeKind = "references"
	// EdgeDependsOn is a coarse package- or file-level import dependency.
	EdgeDependsOn EdgeKind = "dependsOn"
	// EdgeReturns links a function/method to types in its return signature.
	EdgeReturns EdgeKind = "returns"
	// EdgeUsesType links a function/method body to a named loaded type it
	// explicitly constructs, declares, converts to, or asserts.
	EdgeUsesType EdgeKind = "usesType"
)

// AllEdgeKinds returns every edge kind archmotif recognises in Stage 1.
// (Reads/Writes for fields are deferred — see ROADMAP Stage 1 and the PR
// description.)
func AllEdgeKinds() []EdgeKind {
	return []EdgeKind{
		EdgeContains,
		EdgeImplements,
		EdgeEmbeds,
		EdgeCalls,
		EdgeCallsFrom,
		EdgeReferences,
		EdgeDependsOn,
		EdgeReturns,
		EdgeUsesType,
	}
}

// Direction selects in- or out-neighbours for queries.
type Direction int

const (
	// DirectionOut walks outgoing edges (from the node).
	DirectionOut Direction = iota
	// DirectionIn walks incoming edges (to the node).
	DirectionIn
	// DirectionBoth walks both directions.
	DirectionBoth
)
