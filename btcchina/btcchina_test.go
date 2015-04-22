package btcchina

import (
	"bitfx/exchange"
	"math"
	"os"
	"testing"
)

var (
	book   exchange.Book
	client = New(os.Getenv("BTC_KEY"), os.Getenv("BTC_SECRET"), "btc", "cny", 1, 0.002, 2, .1)
)

// Used for float equality
func notEqual(f1, f2 float64) bool {
	if math.Abs(f1-f2) > 0.000001 {
		return true
	}
	return false
}

func TestPriority(t *testing.T) {
	if client.Priority() != 1 {
		t.Fatal("Priority should be 1")
	}
}

func TestFee(t *testing.T) {
	if notEqual(client.Fee(), 0.002) {
		t.Fatal("Fee should be 0.002")
	}
}

func TestUpdatePositon(t *testing.T) {
	if notEqual(client.Position(), 0) {
		t.Fatal("Should start with zero position")
	}
	client.SetPosition(10)
	if notEqual(client.Position(), 10) {
		t.Fatal("Position should have updated to 10")
	}
}

func TestCurrency(t *testing.T) {
	if client.Currency() != "cny" {
		t.Fatal("Currency should be cny")
	}
}

func TestCurrencyCode(t *testing.T) {
	if client.CurrencyCode() != 1 {
		t.Fatal("Currency code should be 1")
	}
}

func TestMaxPos(t *testing.T) {
	if notEqual(client.MaxPos(), 0) {
		t.Fatal("MaxPos should start at 0")
	}
	client.SetMaxPos(23)
	if notEqual(client.MaxPos(), 23) {
		t.Fatal("MaxPos should be set to 23")
	}
}

func TestAvailFunds(t *testing.T) {
	if notEqual(client.AvailFunds(), 0.1) {
		t.Fatal("Available funds should be 0.1")
	}
}

func TestAvailShort(t *testing.T) {
	if notEqual(client.AvailShort(), 2) {
		t.Fatal("Available short should be 2")
	}
}

func TestHasCryptoFee(t *testing.T) {
	if client.HasCryptoFee() {
		t.Fatal("Should not have cryptocurrency fee")
	}
}

// ***** Live exchange communication tests *****
// Slow... skip when not needed

func TestCommunicateBook(t *testing.T) {
	bookChan := make(chan exchange.Book)
	if book = client.CommunicateBook(bookChan); book.Error != nil {
		t.Fatal(book.Error)
	}

	book = <-bookChan
	t.Logf("Received book data")
	// spew.Dump(book)
	if len(book.Bids) != 5 || len(book.Asks) != 5 {
		t.Fatal("Expected 5 book entries")
	}
	if book.Bids[0].Price < book.Bids[1].Price {
		t.Fatal("Bids not sorted correctly")
	}
	if book.Asks[0].Price > book.Asks[1].Price {
		t.Fatal("Asks not sorted correctly")
	}
}

func TestNewOrder(t *testing.T) {
	action := "buy"
	otype := "limit"
	amount := 0.001
	price := book.Bids[0].Price - 10

	// Test submitting a new order
	id, err := client.SendOrder(action, otype, amount, price)
	if err != nil || id == 0 {
		t.Fatal(err)
	}
	t.Logf("Placed a new buy order of 0.0001 btc_usd @ %v limit with ID: %d", price, id)

	// Check status
	var order exchange.Order
	tryAgain := true
	for tryAgain {
		t.Logf("checking status...")
		order, err = client.GetOrderStatus(id)
		tryAgain = order.Status == ""
	}
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
	success, err := client.CancelOrder(id)
	if err != nil {
		t.Fatal(err)
	}
	if !success {
		t.Fatal("Order not cancelled")
	}
	t.Logf("Sent cancellation")

	// Check status
	tryAgain = true
	for tryAgain {
		t.Logf("checking status...")
		order, err = client.GetOrderStatus(id)
		tryAgain = order.Status == ""
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Order confirmed dead")
	if order.FilledAmount != 0 {
		t.Fatal("Order should not be filled")
	}
	t.Logf("Order confirmed unfilled")

	// Test bad order
	id, err = client.SendOrder("buy", otype, 0, price)
	if id != 0 {
		t.Fatal("Expected id = 0")
	}
	if err == nil {
		t.Fatal("Expected error on bad order")
	}

	client.Done()
}
