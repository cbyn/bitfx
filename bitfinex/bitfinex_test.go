package bitfinex

import (
	// "github.com/davecgh/go-spew/spew"
	"bitfx2/exchange"
	"os"
	"testing"
	"time"
)

var book exchange.Book
var bf = New(os.Getenv("BITFINEX_KEY"), os.Getenv("BITFINEX_SECRET"), "ltc", "usd", 2, 0.001)

func TestPriority(t *testing.T) {
	if bf.Priority() != 2 {
		t.Fatal("Priority should be 2")
	}
}

func TestFee(t *testing.T) {
	if bf.Fee()-0.002 > 0.000001 {
		t.Fatal("Fee should be 0.002")
	}
}

func TestUpdatePositon(t *testing.T) {
	if bf.Position() != 0 {
		t.Fatal("Should start with zero position")
	}
	bf.SetPosition(10)
	t.Logf("Set position to 10")
	if bf.Position() != 10 {
		t.Fatal("Position should have updated to 10")
	}
}

func TestCommunicateBook(t *testing.T) {
	bookChan := make(chan exchange.Book)
	doneChan := make(chan bool)
	err := bf.CommunicateBook(bookChan, doneChan)
	if err != nil {
		t.Fatal(err)
	}

	// Notify doneChan in 5 seconds
	go func() {
		time.Sleep(5 * time.Second)
		doneChan <- true
		t.Logf("Notified doneChan")
	}()

	for book = range bookChan {
		t.Logf("Received book data")
		// spew.Dump(book)
		if len(book.Bids) != 20 || len(book.Asks) != 20 {
			t.Fatal("Expected 20 book entries")
		}
		if book.Bids[0].Price < book.Bids[1].Price {
			t.Fatal("Bids not sorted correctly")
		}
		if book.Asks[0].Price > book.Asks[1].Price {
			t.Fatal("Asks not sorted correctly")
		}
	}
}

func TestNewOrder(t *testing.T) {
	action := "sell"
	otype := "limit"
	amount := 0.1
	price := book.Asks[0].Price + 0.10

	// Test submitting a new order
	id, err := bf.SendOrder(action, otype, amount, price)
	if err != nil || id == 0 {
		t.Fatal(err)
	}
	t.Logf("Placed a new sell order of 0.1 ltcusd @ %v limit with ID: %d", price, id)

	// Check status
	order, err := bf.GetOrderStatus(id)
	if err != nil {
		t.Fatal(err)
	}
	if order.Status != "live" {
		t.Fatal("Order should be live")
	}
	t.Logf("Order confirmed live")
	if order.FilledAmount != 0 {
		t.Fatal("Order should not be filled")
	}
	t.Logf("Order confirmed unfilled")

	// Test cancelling the order
	success, err := bf.CancelOrder(id)
	if err != nil {
		t.Fatal(err)
	}
	if !success {
		t.Fatal("Order not cancelled")
	}
	t.Logf("Sent cancellation")

	// Check status
	tryAgain := true
	for tryAgain {
		t.Logf("checking status...")
		order, err = bf.GetOrderStatus(id)
		tryAgain = order.Status == ""
	}
	if err != nil {
		t.Fatal(err)
	}
	if order.Status != "dead" {
		t.Fatal("Order should be dead after cancel")
	}
	t.Logf("Order confirmed dead")
	if order.FilledAmount != 0 {
		t.Fatal("Order should not be filled")
	}
	t.Logf("Order confirmed unfilled")

	// Test bad order
	id, err = bf.SendOrder("kill", otype, amount, price)
	if id != 0 {
		t.Fatal("Expected id = 0")
	}
	if err.Error() != "Bitfinex SendOrder error: Order side must be either 'buy' or 'sell'." {
		t.Fatal("Expected error 'Bitfinex SendOrder error: Order side must be either 'buy' or 'sell'.' on bad order")
	}
}
