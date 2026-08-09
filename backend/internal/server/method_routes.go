package server

import "net/http"

// registerSingleMethodRoute must only be called once for a path. Multi-method
// resources need one shared fallback with the complete Allow value.
func (s *Server) registerSingleMethodRoute(method string, pattern string, handler http.HandlerFunc, methodNotAllowed http.HandlerFunc) {
	s.mux.HandleFunc(method+" "+pattern, handler)
	if method == http.MethodGet {
		s.mux.HandleFunc(http.MethodHead+" "+pattern, methodNotAllowed)
	}
	s.mux.HandleFunc(pattern, methodNotAllowed)
}

func jsonMethodNotAllowed(allowedMethod string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", allowedMethod)
		writeError(w, r, NewHTTPError(http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed"))
	}
}
