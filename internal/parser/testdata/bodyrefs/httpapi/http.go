package httpapi

import "bodyrefs/api"

type HandlerFunc func()

type Route struct {
	Handler HandlerFunc
}

type handler struct{}

func Routes() []Route {
	h := handler{}
	return []Route{
		{Handler: HandlerFunc(h.handle)},
	}
}

func (h handler) handle() {
	var req api.Request
	response := api.Response{Items: []string{req.ID}}
	use(response)
}

func use(v any) {}
