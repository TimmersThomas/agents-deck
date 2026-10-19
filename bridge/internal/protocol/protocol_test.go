package protocol

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestRenderFrame(t *testing.T) {
	now := time.UnixMilli(1_000_000)
	cards := []Card{{ID: "s1", Title: "DEV-1-fix", Status: Waiting, SinceMs: 999_000}}

	got, err := Render(cards, now)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"v":1,"t":"state","now_ms":1000000,"total":1,"cards":[{"id":"s1","title":"DEV-1-fix","status":"waiting","since_ms":999000}]}` + "\n"
	if string(got) != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestRenderEmptyCardsIsArray(t *testing.T) {
	got, err := Render(nil, time.UnixMilli(5))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"v":1,"t":"state","now_ms":5,"total":0,"cards":[]}` + "\n"
	if string(got) != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

type decoded struct {
	Total int    `json:"total"`
	Cards []Card `json:"cards"`
}

func render(t *testing.T, cards []Card) decoded {
	t.Helper()
	b, err := Render(cards, time.UnixMilli(1))
	if err != nil {
		t.Fatal(err)
	}
	var d decoded
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestRenderKeepsFirstMaxCardsAndReportsTotal(t *testing.T) {
	var cards []Card
	for i := 0; i < MaxCards+4; i++ {
		cards = append(cards, Card{ID: fmt.Sprintf("s%02d", i), Title: "t", Status: Idle})
	}
	d := render(t, cards)
	if d.Total != MaxCards+4 || len(d.Cards) != MaxCards {
		t.Fatalf("total %d, %d cards; want %d, %d", d.Total, len(d.Cards), MaxCards+4, MaxCards)
	}
	for i, c := range d.Cards {
		if c.ID != cards[i].ID {
			t.Fatalf("card %d is %s, want %s", i, c.ID, cards[i].ID)
		}
	}
}

func TestRenderTruncatesTitleOnRuneBoundary(t *testing.T) {
	long := strings.Repeat("a", MaxTitleRunes-1) + "éüx"
	cards := []Card{{ID: "s1", Title: long, Status: Idle}, {ID: "s2", Title: "short", Status: Idle}}
	d := render(t, cards)
	want := strings.Repeat("a", MaxTitleRunes-1) + "é"
	if got := d.Cards[0].Title; got != want || utf8.RuneCountInString(got) != MaxTitleRunes {
		t.Fatalf("title %q, want %q", got, want)
	}
	if d.Cards[1].Title != "short" {
		t.Fatalf("short title changed to %q", d.Cards[1].Title)
	}
	if cards[0].Title != long {
		t.Fatal("Render mutated its input")
	}
}

func TestWorstCaseFrameMatchesDocumentedBound(t *testing.T) {
	titles := []string{
		strings.Repeat("<", MaxTitleRunes+5),
		strings.Repeat("\x01", MaxTitleRunes+5),
		strings.Repeat(">", MaxTitleRunes+5),
		strings.Repeat(" ", MaxTitleRunes+5),
	}
	var cards []Card
	for i := 0; i < MaxCards+1; i++ {
		cards = append(cards, Card{
			ID:      strings.Repeat("&", MaxIDBytes),
			Title:   titles[i%len(titles)],
			Status:  Working,
			SinceMs: -1 << 63,
		})
	}
	b, err := Render(cards, time.UnixMilli(-1<<63))
	if err != nil {
		t.Fatal(err)
	}
	// The bound reserves 19 digits for total; this frame's total has 2.
	if want := MaxFrameBytes - 19 + 2; len(b) != want {
		t.Fatalf("worst-case frame is %d bytes, want %d (bound %d)", len(b), want, MaxFrameBytes)
	}
}
