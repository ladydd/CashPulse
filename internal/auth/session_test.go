package auth

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLoginGuardGlobalLock(t *testing.T) {
	g := NewLoginGuard()
	g.maxGlobalFails = 3
	g.globalLockFor = time.Minute
	g.baseDelay = 0
	for i := 0; i < 3; i++ {
		ip := fmt.Sprintf("1.2.3.%d", i)
		locked, _ := g.Fail(ip)
		if i < 2 && locked {
			t.Fatalf("locked too early at %d", i)
		}
		if i == 2 && !locked {
			t.Fatal("expected global lock on 3rd fail")
		}
	}
	ok, retry := g.Allow("9.9.9.9")
	if ok || retry <= 0 {
		t.Fatalf("expected deny during lock ok=%v retry=%v", ok, retry)
	}
}

func TestSessionCreateValid(t *testing.T) {
	s := NewStore("secret-pass", time.Hour, false, nil)
	rr := httptest.NewRecorder()
	id, err := s.Create(rr)
	if err != nil || id == "" {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, c := range rr.Result().Cookies() {
		req.AddCookie(c)
	}
	if !s.Valid(req) {
		t.Fatal("session should be valid")
	}
	if !s.CheckPassword("secret-pass") || s.CheckPassword("wrong") {
		t.Fatal("password check")
	}
}
