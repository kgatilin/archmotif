// PROPOSAL: extract interface from repeated motif (size 3)
// AFFECTED: pkg/store/user.go, pkg/store/order.go, pkg/store/product.go
//
// Generated skeleton for ADR-016. The on-disk file is valid Go so
// go/parser accepts it; role placeholders appear as bare identifiers.
// The angle-bracket display form (<Role>) is reconstructed from the
// // ROLE comments at prompt-build time (Stage 7).

package motif001

// ROLE Iface : interface
// ROLE Method : method on Impl realising Iface.Method
// ROLE Param : parameter name on the realised method
// ROLE ParamType : parameter type on the realised method
// ROLE RetType : return type on the realised method
type Iface interface {
	Method(Param ParamType) RetType
}

// ROLE Impl : struct
type Impl struct{}

// ROLE Method (continued): definition on Impl realising Iface.Method.
func (i *Impl) Method(Param ParamType) RetType { return RetType{} }

// Stub types so the file is self-contained and go/parser-clean.
// These are not roles; they exist only to give Param/RetType something
// to refer to in the placeholder world.
type ParamType struct{}
type RetType struct{}

// SAMPLES:
//   Iface=UserStore    Impl=SQLUserStore   Method=Find
//   Iface=OrderStore   Impl=SQLOrderStore  Method=Find
//   Iface=ProductStore Impl=PgProductStore Method=Lookup
