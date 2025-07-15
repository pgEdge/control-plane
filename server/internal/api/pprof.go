package api

import (
	"net/http"
	"net/http/pprof"

	goahttp "goa.design/goa/v3/http"
)

// See https://pkg.go.dev/net/http/pprof for how to use these endpoints.
func mountPprofHandlers(mux goahttp.Muxer) {
	mux.Handle("GET", "/debug/pprof/profile", pprof.Profile)
	mux.Handle("GET", "/debug/pprof/cmdline", pprof.Cmdline)
	mux.Handle("GET", "/debug/pprof/symbol", pprof.Symbol)
	mux.Handle("GET", "/debug/pprof/trace", pprof.Trace)
	mux.Handle("GET", "/debug/pprof/", func(w http.ResponseWriter, r *http.Request) {
		// Serves a list of available profiles in HTML format
		pprof.Index(w, r)
	})
	mux.Handle("GET", "/debug/pprof/{profile}", func(w http.ResponseWriter, r *http.Request) {
		profile := mux.Vars(r)["profile"]
		pprof.Handler(profile).ServeHTTP(w, r)
	})
}
