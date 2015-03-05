// OKCoin trading API

package okcoin

import (
	"bitfx2/exchange"
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/net/websocket"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	websocketURL = "wss://real.okcoin.com:10440/websocket/okcoinapi"
	restURL      = "https://www.okcoin.com/api/v1/"
	origin       = "http://localhost/"
)

// OKCoin exchange information
type OKCoin struct {
	key, secret, symbol, currency string
	priority                      int
	position, fee                 float64
}

// Exchange request format
type request struct {
	Event      string            `json:"event"`      // Event to request
	Channel    string            `json:"channel"`    // Channel on which to make request
	Parameters map[string]string `json:"parameters"` // Additional parameters
}

// Exchange response format
type response []struct {
	Channel   string          `json:"channel"`          // Channel name
	ErrorCode int64           `json:"errorcode,string"` // Error code if not successful
	Data      json.RawMessage `json:"data"`             // Data specific to channel
}

// New returns a pointer to a new OKCoin instance
func New(key, secret, symbol, currency string, priority int, fee float64) *OKCoin {
	return &OKCoin{
		key:      key,
		secret:   secret,
		symbol:   symbol,
		currency: currency,
		priority: priority,
		fee:      fee,
	}
}

// String implements the Stringer interface
func (ok *OKCoin) String() string {
	return "OKCoin"
}

// Priority returns the exchange priority for order execution
func (ok *OKCoin) Priority() int {
	return ok.priority
}

// Fee returns the exchange order fee
func (ok *OKCoin) Fee() float64 {
	return ok.fee
}

// SetPosition setter method
func (ok *OKCoin) SetPosition(pos float64) {
	ok.position = pos
}

// Position getter method
func (ok *OKCoin) Position() float64 {
	return ok.position
}

// TODO: Incorporate heartbeat

// BookChan returns a channel that receives the latest available book data
func (ok *OKCoin) BookChan(doneChan <-chan bool) (<-chan exchange.Book, error) {
	// Channel to return
	bookChan := make(chan exchange.Book)

	// Connect to websocket
	ws, err := websocket.Dial(websocketURL, "", origin)
	if err != nil {
		return bookChan, err
	}

	// Send request for book data
	channel := fmt.Sprintf("ok_%s%s_depth", ok.symbol, ok.currency)
	message, err := json.Marshal(request{Event: "addChannel", Channel: channel})
	if err != nil {
		return bookChan, err
	}
	_, err = ws.Write(message)
	if err != nil {
		return bookChan, err
	}

	// Run infinite read loop sending results to bookChan
	go func() {
		// Start heartbeat timer
		lastCheck := time.Now()
	Loop:
		for {
			// Check connection
			if time.Since(lastCheck) > 30*time.Second {
				isLive, err := heartbeat(ws)
				if !isLive || err != nil {
					// TODO: handle errors?
					log.Println("OKCoin BookChan lost connection to websocket")
					ws, err = websocket.Dial(websocketURL, "", origin)
					if err != nil {
						log.Printf("OKCoin BookChan reconnect error: %s\n", err.Error())
					} else {
						_, err = ws.Write(message)
						if err != nil {
							log.Printf("OKCoin BookChan reconnect error: %s\n", err.Error())
						}
					}
				}
				lastCheck = time.Now()
			}

			// Break if notified on doneChan
			select {
			case <-doneChan:
				ws.Close()
				close(bookChan)
				break Loop
			default:
				bookChan <- ok.readBook(ws)
			}
		}
	}()

	return bookChan, nil
}

// Check websocket connection
func heartbeat(ws *websocket.Conn) (bool, error) {
	// Send ping
	_, err := ws.Write([]byte("{'event':'ping'}"))
	if err != nil {
		return false, err
	}

	// Read response
	msg := make([]byte, 512)
	n, err := ws.Read(msg)
	if err != nil {
		return false, err
	}
	var req request
	json.Unmarshal(msg[:n], &req)

	// Connection is ok if pong is returned
	if req.Event == "pong" {
		return true, nil
	}

	return false, nil
}

// Read book data from websocket
func (ok *OKCoin) readBook(ws *websocket.Conn) exchange.Book {
	book := exchange.Book{Exg: ok}

	// Read from websocket
	msg := make([]byte, 4096)
	n, err := ws.Read(msg)
	if err != nil {
		book.Error = fmt.Errorf("OKCoin book error: %s", err.Error())
		book.Time = time.Now()
		return book
	}

	// Unmarshal into response
	var resp response
	err = json.Unmarshal(msg[:n], &resp)
	if err != nil {
		book.Error = fmt.Errorf("OKCoin book error: %s", err.Error())
		book.Time = time.Now()
		return book
	}
	if resp[0].ErrorCode != 0 {
		book.Error = fmt.Errorf("OKCoin book error code: %d", resp[0].ErrorCode)
		book.Time = time.Now()
		return book
	}

	// Book structure from the exchange
	var tmp struct {
		Bids       [][2]float64 `json:"bids"`             // Slice of bid data items
		Asks       [][2]float64 `json:"asks"`             // Slice of ask data items
		Timestamp  int64        `json:"timestamp,string"` // Timestamp
		UnitAmount int          `json:"unit_amount"`      // Unit amount for futures
	}

	// Unmarshal response.Data into intermediate structure
	err = json.Unmarshal(resp[0].Data, &tmp)
	if err != nil {
		book.Error = fmt.Errorf("OKCoin book error: %s", err.Error())
		book.Time = time.Now()
		return book
	}

	// Translate into exchange.Book structure
	book.Bids = make(exchange.BidItems, 20)
	book.Asks = make(exchange.AskItems, 20)
	for i := 0; i < 20; i++ {
		book.Bids[i].Price = tmp.Bids[i][0]
		book.Bids[i].Amount = tmp.Bids[i][1]
		book.Asks[i].Price = tmp.Asks[i][0]
		book.Asks[i].Amount = tmp.Asks[i][1]
	}
	sort.Sort(book.Bids)
	sort.Sort(book.Asks)

	book.Error = nil
	book.Time = time.Now()
	return book
}

// SendOrder to the exchange
func (ok *OKCoin) SendOrder(action, otype string, amount, price float64) (int64, error) {
	// Create parameter map for signing
	params := make(map[string]string)
	params["symbol"] = fmt.Sprintf("%s_%s", ok.symbol, ok.currency)
	if otype == "limit" {
		params["type"] = action
	} else if otype == "market" {
		params["type"] = fmt.Sprintf("%s_%s", action, otype)
	}
	params["price"] = fmt.Sprintf("%f", price)
	params["amount"] = fmt.Sprintf("%f", amount)

	// Send post request
	data, err := ok.post(restURL+"trade.do", params)
	if err != nil {
		return 0, errors.New("OKCoin SendOrder error: " + err.Error())
	}

	// Unmarshal response
	var response struct {
		ID        int64 `json:"order_id"`
		ErrorCode int64 `json:"error_code"`
	}
	err = json.Unmarshal(data, &response)
	if err != nil {
		return 0, errors.New("OKCoin SendOrder error: " + err.Error())
	}
	if response.ErrorCode != 0 {
		return 0, fmt.Errorf("OKCoin SendOrder error code: %d", response.ErrorCode)
	}

	return response.ID, nil
}

// CancelOrder on the exchange
func (ok *OKCoin) CancelOrder(id int64) (bool, error) {
	// Create parameter map for signing
	params := make(map[string]string)
	params["symbol"] = fmt.Sprintf("%s_%s", ok.symbol, ok.currency)
	params["order_id"] = fmt.Sprintf("%d", id)

	// Send post request
	data, err := ok.post(restURL+"cancel_order.do", params)
	if err != nil {
		return false, errors.New("OKCoin CancelOrder error: " + err.Error())
	}

	// Unmarshal response
	var response struct {
		Result    bool  `json:"result"`
		ErrorCode int64 `json:"error_code"`
	}
	err = json.Unmarshal(data, &response)
	if err != nil {
		return false, errors.New("OKCoin CancelOrder error: " + err.Error())
	}
	if response.ErrorCode != 0 {
		return false, fmt.Errorf("OKCoin CancelOrder error code: %d", response.ErrorCode)
	}

	return response.Result, nil
}

// GetOrderStatus of an order on the exchange
func (ok *OKCoin) GetOrderStatus(id int64) (exchange.Order, error) {
	// Create parameter map for signing
	params := make(map[string]string)
	params["symbol"] = fmt.Sprintf("%s_%s", ok.symbol, ok.currency)
	params["order_id"] = fmt.Sprintf("%d", id)

	// Create order to be returned
	var order exchange.Order

	// Send post request
	data, err := ok.post(restURL+"order_info.do", params)
	if err != nil {
		return order, errors.New("OKCoin GetOrderStatus error: " + err.Error())
	}

	// Unmarshal response
	var response struct {
		Orders []struct {
			Status     int     `json:"status"`
			DealAmount float64 `json:"deal_amount"`
		} `json:"orders"`
		ErrorCode int64 `json:"error_code"`
	}
	err = json.Unmarshal(data, &response)
	if err != nil {
		return order, errors.New("OKCoin GetOrderStatus error: " + err.Error())
	}
	if response.ErrorCode != 0 {
		return order, fmt.Errorf("OKCoin GetOrderStatus error code: %d", response.ErrorCode)
	}

	if response.Orders[0].Status == -1 || response.Orders[0].Status == 2 {
		order.Status = "dead"
	} else if response.Orders[0].Status == 4 || response.Orders[0].Status == 5 {
		order.Status = ""
	} else {
		order.Status = "live"
	}
	order.FilledAmount = math.Abs(response.Orders[0].DealAmount)
	return order, nil

}

func (ok *OKCoin) post(stringrestURL string, params map[string]string) ([]byte, error) {
	// Make url.Values from params
	values := url.Values{}
	for param, value := range params {
		values.Set(param, value)
	}
	// Add authorization key to url.Values
	values.Set("api_key", ok.key)
	// Prepare string to sign with MD5
	stringParams := values.Encode()
	// Add the authorization secret to the end
	stringParams += fmt.Sprintf("&secret_key=%s", ok.secret)
	// Sign with MD5
	sum := md5.Sum([]byte(stringParams))
	// Add sign to url.Values
	values.Set("sign", strings.ToUpper(fmt.Sprintf("%x", sum)))

	// Send post request
	resp, err := http.PostForm(stringrestURL, values)
	if err != nil {
		return []byte{}, err
	}
	defer resp.Body.Close()

	return ioutil.ReadAll(resp.Body)

}

// unauthenticated get
func get(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return []byte{}, err
	}
	if resp.StatusCode != 200 {
		return []byte{}, errors.New(resp.Status)
	}
	defer resp.Body.Close()

	return ioutil.ReadAll(resp.Body)
}
