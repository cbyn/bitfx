// Cryptocurrency exchange abstraction

package exchange

import (
	"time"
)

// Exchange methods for data and trading ***************************************
type Exchange interface {
	// Implement Stringer interface
	String() string
	// Return exchange priority for order execution
	// Lower priority number is executed first
	// Equal priority results in concurrent execution
	Priority() int
	// Return percent fee charged for taking a market
	Fee() float64
	// Position setter method
	SetPosition(float64)
	// Return position set above
	Position() float64
	// Max allowed position setter method
	SetMaxPos(float64)
	// Return max allowed position set above
	MaxPos() float64
	// Return fiat currency funds available for purchases
	AvailFunds() float64
	// Return amount of cryptocurrency available for short selling
	AvailShort() float64
	// Return the fiat currency in use
	Currency() string
	// Return the fiat currency code
	// USD = 0
	// CNY = 1
	CurrencyCode() byte
	// Send the latest available exchange.Book on the supplied channel
	CommunicateBook(bookChan chan<- Book) Book
	// Send an order to the exchange
	// action = "buy" or "sell"
	// otype = "limit" or "market"
	SendOrder(action, otype string, amount, price float64) (int64, error)
	// Cancel an existing order on the exchange
	CancelOrder(id int64) (bool, error)
	// Return status of an existing order on the exchange
	GetOrderStatus(id int64) (Order, error)
	// Return true if fees are charged in cryptocurrency on purchases
	HasCryptoFee() bool
	// Close all connections
	Done()
}

// Order status data from the exchange *****************************************
type Order struct {
	FilledAmount float64 // Positive number for buys and sells
	Status       string  // "live" or "dead"
}

// Book data from the exchange *************************************************
type Book struct {
	Exg   Exchange
	Time  time.Time
	Bids  BidItems // Sort by price high to low
	Asks  AskItems // Sort by price low to high
	Error error
}

// BidItems data from the exchange
type BidItems []struct {
	Price  float64
	Amount float64
}

// AskItems data from the exchange
type AskItems []struct {
	Price  float64
	Amount float64
}

// Len implements sort.Interface on BidItems
func (items BidItems) Len() int {
	return len(items)
}

// Swap implements sort.Interface on BidItems
func (items BidItems) Swap(i, j int) {
	items[i], items[j] = items[j], items[i]
}

// Less implements sort.Interface on BidItems
func (items BidItems) Less(i, j int) bool {
	return items[i].Price > items[j].Price
}

// Len implements sort.Interface on AskItems
func (items AskItems) Len() int {
	return len(items)
}

// Swap implements sort.Interface on AskItems
func (items AskItems) Swap(i, j int) {
	items[i], items[j] = items[j], items[i]
}

// Less implements sort.Interface on AskItems
func (items AskItems) Less(i, j int) bool {
	return items[i].Price < items[j].Price
}
