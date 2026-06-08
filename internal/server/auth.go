package server

import "net/http"

// RequireForwardAuth returns middleware that gates a handler on a trusted
// forward-auth header. An upstream proxy (e.g. Authentik via Ingress
// forward-auth) is expected to authenticate the request and set header to the
// resolved user; the gateway itself does not authenticate. When the header is
// present and non-empty the request is passed to next; otherwise it is rejected
// with 403 and next is never called.
//
// The header name is configuration (config.Config.AuthHeader) so the same image
// installs into any cluster regardless of the proxy's chosen header.
func RequireForwardAuth(header string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get(header) == "" {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
