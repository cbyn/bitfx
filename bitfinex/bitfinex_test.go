package bitfinex

import (
	"bitfx2/exchange"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// Returns a mock HTTP server
func testServer(code int, body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(code)
		fmt.Fprintln(w, body)
	}))
}

// Used for float equality
func notEqual(f1, f2 float64) bool {
	if math.Abs(f1-f2) > 0.000001 {
		return true
	}
	return false
}

// Test retrieving book data with mock server
func TestGetBook(t *testing.T) {
	body := `{"bids":[{"price":"1.6391","amount":"53.08276864","timestamp":"1427811013.0"},{"price":"1.639","amount":"13.62","timestamp":"1427810280.0"},{"price":"1.638","amount":"14.26","timestamp":"1427810251.0"},{"price":"1.637","amount":"8.44","timestamp":"1427810231.0"},{"price":"1.636","amount":"21.43","timestamp":"1427810216.0"},{"price":"1.634","amount":"9.96","timestamp":"1427810238.0"},{"price":"1.631","amount":"11.7","timestamp":"1427809353.0"},{"price":"1.63","amount":"0.1","timestamp":"1427788892.0"},{"price":"1.629","amount":"6.98","timestamp":"1427809000.0"},{"price":"1.628","amount":"11.7","timestamp":"1427809359.0"},{"price":"1.627","amount":"25.91512719","timestamp":"1427808956.0"},{"price":"1.6269","amount":"13.54211743","timestamp":"1427811077.0"},{"price":"1.626","amount":"6.98","timestamp":"1427808940.0"},{"price":"1.625","amount":"11.7","timestamp":"1427809365.0"},{"price":"1.6233","amount":"0.1","timestamp":"1427680917.0"},{"price":"1.622","amount":"15.68","timestamp":"1427808196.0"},{"price":"1.6201","amount":"174.0","timestamp":"1427810992.0"},{"price":"1.62","amount":"119.94830228","timestamp":"1427810640.0"},{"price":"1.6159","amount":"200.0","timestamp":"1427811056.0"},{"price":"1.6157","amount":"2151.8","timestamp":"1427811049.0"}],"asks":[{"price":"1.649","amount":"8.225777","timestamp":"1427811011.0"},{"price":"1.65","amount":"118.35905692","timestamp":"1427807969.0"},{"price":"1.651","amount":"56.3099955","timestamp":"1427810969.0"},{"price":"1.652","amount":"21.79","timestamp":"1427810806.0"},{"price":"1.653","amount":"21.29","timestamp":"1427810776.0"},{"price":"1.654","amount":"21.1","timestamp":"1427811017.0"},{"price":"1.655","amount":"21.69","timestamp":"1427810883.0"},{"price":"1.656","amount":"19.45","timestamp":"1427810790.0"},{"price":"1.657","amount":"27.1030322","timestamp":"1427803455.0"},{"price":"1.658","amount":"21.69","timestamp":"1427810824.0"},{"price":"1.659","amount":"26.8","timestamp":"1427810129.0"},{"price":"1.66","amount":"27.20087772","timestamp":"1427800329.0"},{"price":"1.661","amount":"21.69","timestamp":"1427810843.0"},{"price":"1.662","amount":"44.3","timestamp":"1427811018.0"},{"price":"1.6792","amount":"3.0","timestamp":"1427808043.0"},{"price":"1.68","amount":"119.94830228","timestamp":"1427810640.0"},{"price":"1.681","amount":"7.1386","timestamp":"1427784448.0"},{"price":"1.684","amount":"10.0","timestamp":"1427771020.0"},{"price":"1.6868","amount":"100.0","timestamp":"1427787418.0"},{"price":"1.6935","amount":"200.0","timestamp":"1427811056.0"}]}`
	server := testServer(200, body)
	client := Client{baseURL: server.URL}
	book, timeStamps := client.getBook()
	if len(timeStamps) != 40 || len(book.Bids) != 20 || len(book.Asks) != 20 {
		t.Fatal("Should have returned 20 items")
	}
	if notEqual(book.Bids[0].Price, 1.6391) || notEqual(book.Bids[19].Price, 1.6157) {
		t.Fatal("Bids not sorted properly")
	}
	if notEqual(book.Asks[0].Price, 1.649) || notEqual(book.Asks[19].Price, 1.6935) {
		t.Fatal("Asks not sorted properly")
	}
}

func TestPriority(t *testing.T) {
	client := Client{priority: 2}
	if client.Priority() != 2 {
		t.Fatal("Priority should be 2")
	}
}

func TestFee(t *testing.T) {
	client := Client{fee: 0.002}
	if notEqual(client.Fee(), 0.002) {
		t.Fatal("Fee should be 0.002")
	}
}

func TestUpdatePositon(t *testing.T) {
	client := Client{position: 0}
	if notEqual(client.Position(), 0) {
		t.Fatal("Should start with zero position")
	}
	client.SetPosition(10)
	t.Logf("Set position to 10")
	if notEqual(client.Position(), 10) {
		t.Fatal("Position should have updated to 10")
	}
}

func TestCurrencyCode(t *testing.T) {
	client := Client{currencyCode: 1}
	if client.CurrencyCode() != 1 {
		t.Fatal("Currency code should be 1")
	}
}

func TestMaxPos(t *testing.T) {
	client := Client{maxPos: 100}
	if notEqual(client.MaxPos(), 100) {
		t.Fatal("MaxPos should be 100")
	}
}

// ***** Live exchange communication tests *****
// Slow... skip when not needed

var book exchange.Book
var bf = New(os.Getenv("BITFINEX_KEY"), os.Getenv("BITFINEX_SECRET"), "ltc", "usd", 2, 0.001, .1, 2)

func TestCommunicateBook(t *testing.T) {
	bookChan := make(chan exchange.Book)
	doneChan := make(chan bool)
	if book = bf.CommunicateBook(bookChan, doneChan); book.Error != nil {
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
	doneChan <- true
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
}
