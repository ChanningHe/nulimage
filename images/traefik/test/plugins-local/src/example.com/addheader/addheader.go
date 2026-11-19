// Package addheader is a minimal Traefik Yaegi middleware used by the image
// test: it sets X-Nimg-Plugin to the configured value on every response.
package addheader

import (
	"context"
	"net/http"
)

// Config is the plugin configuration.
type Config struct {
	Value string `json:"value,omitempty"`
}

// CreateConfig returns the default configuration.
func CreateConfig() *Config {
	return &Config{}
}

type addHeader struct {
	next  http.Handler
	value string
}

// New creates the middleware.
func New(_ context.Context, next http.Handler, config *Config, _ string) (http.Handler, error) {
	return &addHeader{next: next, value: config.Value}, nil
}

func (a *addHeader) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	rw.Header().Set("X-Nimg-Plugin", a.value)
	a.next.ServeHTTP(rw, req)
}
