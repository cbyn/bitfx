package okcoin

import (
	"bitfx/exchange"
	"math"
	"os"
	"testing"
)

var (
	book   exchange.Book
	client = New(os.Getenv("OKUSD_KEY"), os.Getenv("OKUSD_SECRET"), "ltc", "usd", 1, 0.002, 2, .1)
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
	if client.Currency() != "usd" {
		t.Fatal("Currency should be usd")
	}
}

func TestCurrencyCode(t *testing.T) {
	if client.CurrencyCode() != 0 {
		t.Fatal("Currency code should be 0")
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
	if !client.HasCryptoFee() {
		t.Fatal("Should have cryptocurrency fee")
	}
}

// ***** Live exchange communication tests *****
// Slow... skip when not needed

// USD tesing

func TestCommunicateBookUSD(t *testing.T) {
	bookChan := make(chan exchange.Book)
	if book = client.CommunicateBook(bookChan); book.Error != nil {
		t.Fatal(book.Error)
	}

	book = <-bookChan
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

func TestNewOrderUSD(t *testing.T) {
	action := "buy"
	otype := "limit"
	amount := 0.1
	price := book.Bids[0].Price - 0.20

	// Test submitting a new order
	id, err := client.SendOrder(action, otype, amount, price)
	if err != nil || id == 0 {
		t.Fatal(err)
	}
	t.Logf("Placed a new buy order of 0.1 ltc_usd @ %v limit with ID: %d", price, id)

	// Check status
	order, err := client.GetOrderStatus(id)
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
	tryAgain := true
	for tryAgain {
		t.Logf("checking status...")
		order, err = client.GetOrderStatus(id)
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
	id, err = client.SendOrder("kill", otype, amount, price)
	if id != 0 {
		t.Fatal("Expected id = 0")
	}
	if err == nil {
		t.Fatal("Expected error on bad order")
	}

	client.Done()
}

// CNY tesing

func TestCurrencyCodeCNY(t *testing.T) {
	// Reset global variables
	book = exchange.Book{}
	client = New(os.Getenv("OKCNY_KEY"), os.Getenv("OKCNY_SECRET"), "ltc", "cny", 1, 0.002, 2, .1)

	if client.CurrencyCode() != 1 {
		t.Fatal("Currency code should be 1")
	}
}

func TestCommunicateBookCNY(t *testing.T) {
	bookChan := make(chan exchange.Book)
	if book = client.CommunicateBook(bookChan); book.Error != nil {
		t.Fatal(book.Error)
	}

	book = <-bookChan
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

func TestNewOrderCNY(t *testing.T) {
	action := "buy"
	otype := "limit"
	amount := 0.1
	price := book.Bids[0].Price - 1

	// Test submitting a new order
	id, err := client.SendOrder(action, otype, amount, price)
	if err != nil || id == 0 {
		t.Fatal(err)
	}
	t.Logf("Placed a new buy order of 0.1 ltc_cny @ %v limit with ID: %d", price, id)

	// Check status
	order, err := client.GetOrderStatus(id)
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
	tryAgain := true
	for tryAgain {
		t.Logf("checking status...")
		order, err = client.GetOrderStatus(id)
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
	id, err = client.SendOrder("kill", otype, amount, price)
	if id != 0 {
		t.Fatal("Expected id = 0")
	}
	if err == nil {
		t.Fatal("Expected error on bad order")
	}

	client.Done()
}
