package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStaticErrorsKeepHeaders(t *testing.T) {
	s, _, _, _ := fixture(t)
	for _, tc := range []struct {
		path, rangeHeader string
		code              int
	}{{"/static/missing", "", 404}, {"/static/app.js", "bytes=999999999-", 416}} {
		r := httptest.NewRequest("GET", "http://"+s.opts.Host+tc.path, nil)
		r.AddCookie(&http.Cookie{Name: s.cookieName, Value: s.token})
		r.Header.Set("Range", tc.rangeHeader)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != tc.code || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Security-Policy") != csp {
			t.Errorf("%s: %d %v", tc.path, w.Code, w.Header())
		}
	}
}
