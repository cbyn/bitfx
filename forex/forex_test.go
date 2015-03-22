package forex

import (
	// "github.com/davecgh/go-spew/spew"
	"testing"
)

func TestGetQuote(t *testing.T) {
	quote := getQuote("cny")
	if quote.Error != nil {
		t.Fatal(quote.Error)
	}
	// spew.Dump(quote)
}

func TestCommunicateFX(t *testing.T) {
	fxChan := make(chan Quote)
	doneChan := make(chan bool)
	_, err := CommunicateFX("cny", fxChan, doneChan)
	if err != nil {
		t.Fatal(err)
	}

	quote := <-fxChan
	t.Logf("Received quote")
	// spew.Dump(quote)
	if quote.Error != nil {
		t.Fatal(quote.Error)
	}
	quote = <-fxChan
	t.Logf("Received quote")
	// spew.Dump(quote)
	if quote.Error != nil {
		t.Fatal(quote.Error)
	}
}
