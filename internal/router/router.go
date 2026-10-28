package router

import (
	"fmt"
	"net/http"
)

type CustomMux struct {
	mappings map[string]map[string]func(w http.ResponseWriter, r *http.Request)
}

func NewCustomMux() *CustomMux {
	return &CustomMux{make(map[string]map[string]func(w http.ResponseWriter, r *http.Request), 8)}
}

func (c *CustomMux) Handle(method string, pattern string, handler http.Handler) {

	if c.mappings[method][pattern] == nil {
		if c.mappings[method] == nil {
			c.mappings[method] = make(map[string]func(w http.ResponseWriter, r *http.Request))
		}
		c.mappings[method][pattern] = handler.ServeHTTP
		fmt.Println("added path", pattern, "with method", method)
		return
	}
	fmt.Println("path is already defined", pattern)
}

func (c *CustomMux) HandleFunc(method string, pattern string, f func(http.ResponseWriter, *http.Request)) {
	if c.mappings[method][pattern] == nil {

		if c.mappings[method] == nil {
			c.mappings[method] = make(map[string]func(w http.ResponseWriter, r *http.Request))
		}
		c.mappings[method][pattern] = f
		fmt.Println("added path", pattern, "with method", method)
		return
	}
	fmt.Println("path is already defined", pattern)
}

func (c CustomMux) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	a := c.mappings[request.Method][request.URL.Path]
	if a != nil {
		a(writer, request)
	} else {
		fmt.Println("path is not defined", request.URL.Path)
	}
}
