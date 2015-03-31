package forex

import (
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
	if quote := CommunicateFX("cny", fxChan, doneChan); quote.Error != nil {
		t.Fatal(quote.Error)
	}

	if quote := <-fxChan; quote.Error != nil {
		t.Fatal(quote.Error)
	}
	t.Logf("Received quote")
	// spew.Dump(quote)
}
