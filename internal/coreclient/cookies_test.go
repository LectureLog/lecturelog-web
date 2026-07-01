package coreclient

import (
	"context"
	"strings"
	"testing"
)

func TestGetYouTubeCookieStatus(t *testing.T) {
	m := newMockCore(t)
	defer m.srv.Close()
	c := newTestClient(t, m.srv.URL)
	st, err := c.GetYouTubeCookieStatus(context.Background())
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if !st.Exists || st.Size != 120 {
		t.Errorf("статус: got %+v", st)
	}
}

func TestPutYouTubeCookies(t *testing.T) {
	m := newMockCore(t)
	defer m.srv.Close()
	c := newTestClient(t, m.srv.URL)
	st, err := c.PutYouTubeCookies(context.Background(), []byte("abc"))
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if !strings.HasPrefix(m.lastCookieCT, "multipart/form-data") {
		t.Errorf("ожидался multipart, получили %q", m.lastCookieCT)
	}
	if string(m.lastCookieBody) != "abc" {
		t.Errorf("тело cookies: got %q", m.lastCookieBody)
	}
	if !st.Exists {
		t.Errorf("ожидался exists=true")
	}
}

func TestDeleteYouTubeCookies(t *testing.T) {
	m := newMockCore(t)
	defer m.srv.Close()
	c := newTestClient(t, m.srv.URL)
	st, err := c.DeleteYouTubeCookies(context.Background())
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if st.Exists {
		t.Errorf("после удаления ожидался exists=false, got %+v", st)
	}
}
