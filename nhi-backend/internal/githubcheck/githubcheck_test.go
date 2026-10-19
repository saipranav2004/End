package githubcheck

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeGitHub struct {
	repos     string // JSON array for /user/repos
	more      bool
	contents  int // status for /repos/.../contents/
	userCode  int
	redirect  string
	seenAuth  []string
	seenPaths []string
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.seenAuth = append(f.seenAuth, r.Header.Get("Authorization"))
	f.seenPaths = append(f.seenPaths, r.URL.Path)
	if r.Header.Get("Authorization") != "Bearer good" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	switch {
	case r.URL.Path == "/user":
		if f.redirect != "" {
			http.Redirect(w, r, f.redirect, http.StatusFound)
			return
		}
		if f.userCode != 0 {
			w.WriteHeader(f.userCode)
			return
		}
		_, _ = io.WriteString(w, `{"login":"bot"}`)
	case r.URL.Path == "/user/repos":
		if f.more {
			w.Header().Set("Link", `<https://x/user/repos?page=2>; rel="next"`)
		}
		_, _ = io.WriteString(w, f.repos)
	case strings.HasPrefix(r.URL.Path, "/repos/acme/api/contents"):
		w.WriteHeader(f.contents)
		_, _ = io.WriteString(w, `[]`)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func check(t *testing.T, f *fakeGitHub, token string) (Result, error) {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return New(srv.URL).Check(context.Background(), token)
}

func TestCheck(t *testing.T) {
	ok := `[{"full_name":"acme/api"},{"full_name":"acme/web"}]`
	res, err := check(t, &fakeGitHub{repos: ok, contents: 200}, "good")
	if err != nil || res.Login != "bot" || res.Repositories != 2 || res.More {
		t.Fatalf("got %+v, %v", res, err)
	}
	if res, err := check(t, &fakeGitHub{repos: ok, more: true, contents: 200}, "good"); err != nil || !res.More {
		t.Fatalf("more: %+v %v", res, err)
	}
	cases := []struct {
		name  string
		f     *fakeGitHub
		token string
		want  error
	}{
		{"rejected", &fakeGitHub{repos: ok, contents: 200}, "bad", ErrTokenRejected},
		{"blank", &fakeGitHub{repos: ok, contents: 200}, "  ", ErrTokenRejected},
		{"no repositories", &fakeGitHub{repos: `[]`, contents: 200}, "good", ErrNoRepositories},
		{"metadata only", &fakeGitHub{repos: ok, contents: 403}, "good", ErrNoContents},
		{"rate limited", &fakeGitHub{repos: ok, userCode: 429}, "good", ErrUnavailable},
		{"server error", &fakeGitHub{repos: ok, userCode: 502}, "good", ErrUnavailable},
	}
	for _, tc := range cases {
		if _, err := check(t, tc.f, tc.token); !errors.Is(err, tc.want) {
			t.Fatalf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}
	// An empty repository has no contents to read; that is not a failure.
	if _, err := check(t, &fakeGitHub{repos: ok, contents: 404}, "good"); err != nil {
		t.Fatalf("empty repository: %v", err)
	}
}

func TestTokenNeverFollowsARedirect(t *testing.T) {
	elsewhere := &fakeGitHub{}
	other := httptest.NewServer(elsewhere)
	defer other.Close()
	_, err := check(t, &fakeGitHub{redirect: other.URL + "/steal"}, "good")
	if err == nil {
		t.Fatal("a redirect must not count as success")
	}
	if len(elsewhere.seenAuth) != 0 {
		t.Fatalf("token sent to another host: %v", elsewhere.seenAuth)
	}
}
