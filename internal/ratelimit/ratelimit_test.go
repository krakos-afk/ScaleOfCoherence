package ratelimit

import (
	"testing"
	"time"
)

func TestAllowBurstThenDeny(t *testing.T) {
	l := New(60, 10)

	for i := 0; i < 10; i++ {
		if ok, _ := l.Allow("ip"); !ok {
			t.Fatalf("request %d denied within burst", i+1)
		}
	}
	ok, retry := l.Allow("ip")
	if ok {
		t.Fatal("request 11 allowed past burst")
	}
	if retry <= 0 || retry > time.Minute {
		t.Fatalf("retry after = %v, want within (0, 1m]", retry)
	}
}

func TestKeysAreIndependent(t *testing.T) {
	l := New(60, 1)
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("first key denied")
	}
	if ok, _ := l.Allow("a"); ok {
		t.Fatal("second call for key a allowed past burst")
	}
	if ok, _ := l.Allow("b"); !ok {
		t.Fatal("key b affected by key a")
	}
}

func TestRefillAfterDelay(t *testing.T) {
	l := New(600, 1) // one token every 100ms
	if ok, _ := l.Allow("k"); !ok {
		t.Fatal("first token denied")
	}
	ok, retry := l.Allow("k")
	if ok {
		t.Fatal("token available before refill")
	}
	time.Sleep(retry + 10*time.Millisecond)
	if ok, _ := l.Allow("k"); !ok {
		t.Fatal("token not refilled after retry delay")
	}
}
