package page_test

import (
	"encoding/base64"
	"errors"
	"testing"

	"github.com/UrielJaloto/paygo/internal/domain/page"
)

const validID = "01890a5d-ac96-774b-bcce-b302099a8057"

func TestNewLimit_EnforcesRange(t *testing.T) {
	cases := []struct {
		name    string
		value   int
		want    page.Limit
		wantErr error
	}{
		{name: "negative", value: -1, wantErr: page.ErrInvalidPageLimit},
		{name: "zero", value: 0, wantErr: page.ErrInvalidPageLimit},
		{name: "minimum", value: 1, want: 1},
		{name: "maximum", value: 100, want: 100},
		{name: "above maximum", value: 101, wantErr: page.ErrInvalidPageLimit},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := page.NewLimit(tc.value)
			if got != tc.want || !errors.Is(err, tc.wantErr) {
				t.Fatalf("got (%d, %v), want (%d, %v)", got, err, tc.want, tc.wantErr)
			}
		})
	}
}

func TestParseLimit_ReturnsLimitOrErrInvalidPageLimit(t *testing.T) {
	cases := []struct {
		name    string
		text    string
		want    page.Limit
		wantErr error
	}{
		{name: "empty uses default", text: "", want: page.DefaultLimit},
		{name: "valid", text: "50", want: 50},
		{name: "out of range", text: "101", wantErr: page.ErrInvalidPageLimit},
		{name: "not a number", text: "ten", wantErr: page.ErrInvalidPageLimit},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := page.ParseLimit(tc.text)
			if got != tc.want || !errors.Is(err, tc.wantErr) {
				t.Fatalf("got (%d, %v), want (%d, %v)", got, err, tc.want, tc.wantErr)
			}
		})
	}
}

func TestParseCursor_Empty_ReturnsStart(t *testing.T) {
	cursor, err := page.ParseCursor("")
	if err != nil || !cursor.IsStart() || cursor.AfterID() != "" {
		t.Fatalf("got (%+v, %v), want a start cursor", cursor, err)
	}
}

func TestParseCursor_RoundTrip_KeepsAfterID(t *testing.T) {
	cursor, err := page.ParseCursor(page.NewCursor(validID).Encode())
	if err != nil || cursor.AfterID() != validID || cursor.IsStart() {
		t.Fatalf("got (%+v, %v), want after %s", cursor, err, validID)
	}
}

func TestParseCursor_Invalid_ReturnsErrInvalidCursor(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{name: "bad base64", text: "!!"},
		{name: "padded", text: base64.URLEncoding.EncodeToString([]byte(validID + "x"))},
		{name: "not an id", text: base64.RawURLEncoding.EncodeToString([]byte("abc"))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := page.ParseCursor(tc.text)
			if !errors.Is(err, page.ErrInvalidCursor) || got != (page.Cursor{}) {
				t.Fatalf("got (%+v, %v), want ErrInvalidCursor", got, err)
			}
		})
	}
}
