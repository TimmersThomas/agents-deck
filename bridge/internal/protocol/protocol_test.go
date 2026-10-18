package protocol

import (
	"testing"
	"time"
)

func TestRenderFrame(t *testing.T) {
	now := time.UnixMilli(1_000_000)
	cards := []Card{{ID: "s1", Title: "DEV-1-fix", Status: Waiting, SinceMs: 999_000}}

	got, err := Render(cards, now)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"v":1,"t":"state","now_ms":1000000,"cards":[{"id":"s1","title":"DEV-1-fix","status":"waiting","since_ms":999000}]}` + "\n"
	if string(got) != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestRenderEmptyCardsIsArray(t *testing.T) {
	got, err := Render(nil, time.UnixMilli(5))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"v":1,"t":"state","now_ms":5,"cards":[]}` + "\n"
	if string(got) != want {
		t.Fatalf("got %s want %s", got, want)
	}
}
