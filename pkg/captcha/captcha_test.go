package captcha

import "testing"

func TestGenerateVerify(t *testing.T) {
	s := NewStore()
	key, bg, thumb, thumbY, err := s.Generate()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if bg == "" || thumb == "" {
		t.Fatal("empty images")
	}
	if thumbY <= 0 {
		t.Fatal("bad thumbY")
	}

	tx, ok := s.DebugTarget(key)
	if !ok {
		t.Fatal("target missing")
	}
	// correct drag passes
	if _, ok2, _ := s.Verify(key, tx); !ok2 {
		t.Fatal("correct x should pass")
	}
	// consumed -> second use fails
	if _, ok3, _ := s.Verify(key, tx); ok3 {
		t.Fatal("reuse should fail")
	}
	// a far-off drag fails on a fresh challenge
	k2, _, _, _, _ := s.Generate()
	t2, _ := s.DebugTarget(k2)
	if _, ok4, _ := s.Verify(k2, t2+100); ok4 {
		t.Fatal("far x should fail")
	}
}

// TestTicketRoundTrip ensures a ticket issued by Verify can be consumed exactly
// once by UseTicket (regression for the "滑块验证已失效" bug caused by a key
// prefix mismatch in UseTicket).
func TestTicketRoundTrip(t *testing.T) {
	s := NewStore()
	key, _, _, _, err := s.Generate()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	tx, _ := s.DebugTarget(key)
	ticket, ok, _ := s.Verify(key, tx)
	if !ok {
		t.Fatal("verify should succeed")
	}
	if ticket == "" {
		t.Fatal("empty ticket")
	}
	// consuming the ticket (as /auth/send-code does) must succeed
	if !s.UseTicket(ticket) {
		t.Fatal("UseTicket should consume a freshly issued ticket")
	}
	// single-use: a second consume must fail
	if s.UseTicket(ticket) {
		t.Fatal("ticket must not be reusable")
	}
	// a bogus ticket must fail
	if s.UseTicket("cap_does-not-exist") {
		t.Fatal("bogus ticket should fail")
	}
}
