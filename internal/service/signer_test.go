package service

import (
	"testing"
	"time"
)

func TestSignerRoundTrip(t *testing.T) {
	s := newSigner("secret")
	tok := s.sign("stream", "abc123", 3600)
	if tok == "" {
		t.Fatal("empty token")
	}
	if !s.verify("stream", "abc123", tok, time.Now()) {
		t.Fatal("expected token to verify")
	}
}

func TestSignerPurposeBinding(t *testing.T) {
	s := newSigner("secret")
	tok := s.sign("stream", "abc123", 3600)
	if s.verify("cover", "abc123", tok, time.Now()) {
		t.Fatal("token must not verify for a different purpose")
	}
}

func TestSignerWrongID(t *testing.T) {
	s := newSigner("secret")
	tok := s.sign("stream", "abc123", 3600)
	if s.verify("stream", "other", tok, time.Now()) {
		t.Fatal("token must not verify for a different id")
	}
}

func TestSignerTampered(t *testing.T) {
	s := newSigner("secret")
	tok := s.sign("stream", "abc123", 3600)
	if s.verify("stream", "abc123", tok[:len(tok)-2]+"aa", time.Now()) {
		t.Fatal("tampered token must not verify")
	}
}

func TestSignerExpired(t *testing.T) {
	s := newSigner("secret")
	tok := s.sign("stream", "abc123", 60)
	now := time.Now().Add(2 * time.Minute)
	if s.verify("stream", "abc123", tok, now) {
		t.Fatal("expired token must not verify")
	}
}

func TestSignerMalformed(t *testing.T) {
	s := newSigner("secret")
	if s.verify("stream", "abc123", "", time.Now()) {
		t.Fatal("empty token must not verify")
	}
	if s.verify("stream", "abc123", "notatoken", time.Now()) {
		t.Fatal("malformed token must not verify")
	}
}
