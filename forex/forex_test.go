package forex

import (
	// "github.com/davecgh/go-spew/spew"
	"testing"
	"time"
)

func TestGetQuote(t *testing.T) {
	quote := getQuote("cny")
	if quote.Error != nil || quote.Price == 0 {
		t.Fatal("Failed to retreive data")
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

	// Notify doneChan in 5 seconds
	go func() {
		time.Sleep(5 * time.Second)
		doneChan <- true
		t.Logf("Notified doneChan")
	}()

	for quote := range fxChan {
		t.Logf("Received quote")
		// spew.Dump(quote)
		if quote.Error != nil || quote.Price == 0 {
			t.Fatal("Failed to retreive data")
		}
	}

}
