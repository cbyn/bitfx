// Cryptocurrency exchange abstraction

package exchange

// Exchange methods for data and trading ***************************************
type Exchange interface {
	String() string
	Priority() int
	Fee() float64
	SetPosition(float64)
	Position() float64
	UpdateBook(entries int) error
	Book() Book
	SendOrder(action, otype string, amount, price float64) (int64, error)
	CancelOrder(id int64) (bool, error)
	GetOrderStatus(id int64) (Order, error)
}

// Order status data from the exchange *****************************************
type Order struct {
	FilledAmount float64 // Positive number for buys and sells
	Status       string  // "live" or "dead"
}

// Book data from the exchange *************************************************
type Book struct {
	Bids BidItems // Sort by price high to low
	Asks AskItems // Sort by price low to high
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
