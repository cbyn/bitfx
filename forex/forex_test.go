package forex

import (
	"github.com/davecgh/go-spew/spew"
	"testing"
)

func TestGetQuote(t *testing.T) {
	quote, err := GetQuote("CNY=X")
	if err != nil || quote.Price == 0 {
		t.Fatal("Failed to retreive data")
	}
	spew.Dump(quote)
}
