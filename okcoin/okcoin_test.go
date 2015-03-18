package okcoin

import (
	// "github.com/davecgh/go-spew/spew"
	"bitfx2/exchange"
	"os"
	"testing"
	"time"
)

var book exchange.Book
var ok = New(os.Getenv("OKUSD_KEY"), os.Getenv("OKUSD_SECRET"), "ltc", "usd", 1, 0.002)

func TestPriority(t *testing.T) {
	if ok.Priority() != 1 {
		t.Fatal("Priority should be 1")
	}
}

func TestFee(t *testing.T) {
	if ok.Fee()-0.002 > 0.000001 {
		t.Fatal("Fee should be 0.002")
	}
}

func TestUpdatePositon(t *testing.T) {
	if ok.Position() != 0 {
		t.Fatal("Should start with zero position")
	}
	ok.SetPosition(10)
	t.Logf("Set position to 10")
	if ok.Position() != 10 {
		t.Fatal("Position should have updated to 10")
	}
}

// USD tesing ******************************************************************

func TestCurrencyCodeUSD(t *testing.T) {
	if ok.CurrencyCode() != 0 {
		t.Fatal("Currency code should be 0")
	}
}

func TestCommunicateBookUSD(t *testing.T) {
	bookChan := make(chan exchange.Book)
	doneChan := make(chan bool)
	err := ok.CommunicateBook(bookChan, doneChan)
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

func TestNewOrderUSD(t *testing.T) {
	action := "buy"
	otype := "limit"
	amount := 0.1
	price := book.Bids[0].Price - 0.20

	// Test submitting a new order
	id, err := ok.SendOrder(action, otype, amount, price)
	if err != nil || id == 0 {
		t.Fatal(err)
	}
	t.Logf("Placed a new buy order of 0.1 ltc_usd @ %v limit with ID: %d", price, id)

	// Check status
	order, err := ok.GetOrderStatus(id)
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
	success, err := ok.CancelOrder(id)
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
		order, err = ok.GetOrderStatus(id)
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
	id, err = ok.SendOrder("kill", otype, amount, price)
	if id != 0 {
		t.Fatal("Expected id = 0")
	}
	if err == nil {
		t.Fatal("Expected error on bad order")
	}
}

// CNY tesing ******************************************************************

func TestCurrencyCodeCNY(t *testing.T) {
	// Reset global variables
	book = exchange.Book{}
	ok = New(os.Getenv("OKCNY_KEY"), os.Getenv("OKCNY_SECRET"), "ltc", "cny", 1, 0.002)

	if ok.CurrencyCode() != 1 {
		t.Fatal("Currency code should be 1")
	}
}

func TestCommunicateBookCNY(t *testing.T) {
	bookChan := make(chan exchange.Book)
	doneChan := make(chan bool)
	err := ok.CommunicateBook(bookChan, doneChan)
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

func TestNewOrderCNY(t *testing.T) {
	action := "buy"
	otype := "limit"
	amount := 0.1
	price := book.Bids[0].Price - 1

	// Test submitting a new order
	id, err := ok.SendOrder(action, otype, amount, price)
	if err != nil || id == 0 {
		t.Fatal(err)
	}
	t.Logf("Placed a new buy order of 0.1 ltc_cny @ %v limit with ID: %d", price, id)

	// Check status
	order, err := ok.GetOrderStatus(id)
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
	success, err := ok.CancelOrder(id)
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
		order, err = ok.GetOrderStatus(id)
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
	id, err = ok.SendOrder("kill", otype, amount, price)
	if id != 0 {
		t.Fatal("Expected id = 0")
	}
	if err == nil {
		t.Fatal("Expected error on bad order")
	}
}
