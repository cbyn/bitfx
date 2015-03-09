// OKCoin trading API

package okcoin

import (
	"bitfx2/exchange"
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gorilla/websocket"
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

// Result from a websocket Read()
type wsResult struct {
	resp response
	err  error
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

// CommunicateBook sends the latest available book data on the supplied channel
func (ok *OKCoin) CommunicateBook(bookChan chan<- *exchange.Book, doneChan <-chan bool) error {
	// Connect to websocket
	ws, _, err := websocket.DefaultDialer.Dial(websocketURL, http.Header{})
	if err != nil {
		return fmt.Errorf("OKCoin CommunicateBook error: %s", err)
	}

	// Send request for book data
	channel := fmt.Sprintf("ok_%s%s_depth", ok.symbol, ok.currency)
	initMessage := request{Event: "addChannel", Channel: channel}
	if err = ws.WriteJSON(initMessage); err != nil {
		return fmt.Errorf("OKCoin CommunicateBook error: %s", err)
	}

	// Run a read loop in new goroutine
	go ok.runLoop(ws, initMessage, bookChan, doneChan)

	return nil
}

// Websocket read loop
func (ok *OKCoin) runLoop(ws *websocket.Conn, initMessage request, bookChan chan<- *exchange.Book, doneChan <-chan bool) {
	// Setup heartbeat
	pingInterval := 5 * time.Second
	ws.SetPongHandler(func(string) error { return ws.SetReadDeadline(time.Now().Add(pingInterval + time.Second)) })
	ticker := time.NewTicker(pingInterval)

	// Syncronize access to *websocket.Conn
	useWS := make(chan bool)
	getWS := make(chan *websocket.Conn)
	reconnectWS := make(chan bool)
	closeWS := make(chan bool)
	go func() {
	LOOP:
		for {
			select {
			case <-useWS:
				getWS <- ws
			case <-reconnectWS:
				ws = reconnect(initMessage)
			case <-closeWS:
				ws.Close()
				break LOOP
			}
		}
	}()

	// Send pings to websocket
	go func() {
		for range ticker.C {
			useWS <- true
			if err := (<-getWS).WriteControl(9, []byte{}, time.Now().Add(time.Second)); err != nil {
				// Reconnect on error
				reconnectWS <- true
			}
		}
	}()

	// Read from websocket
	respChan := make(chan *response)
	go func() {
		var resp response
		for {
			useWS <- true
			if err := (<-getWS).ReadJSON(&resp); err != nil {
				// Reconnect on error
				reconnectWS <- true
			} else {
				respChan <- &resp
			}
		}
	}()

	for {
		select {
		// End if notified on doneChan
		case <-doneChan:
			close(bookChan)
			closeWS <- true
			return
		case resp := <-respChan:
			bookChan <- ok.convertToBook(resp)
		}
	}
}

// Reconnect websocket
func reconnect(initMessage request) *websocket.Conn {
	log.Println("Reconnecting...")

	// Try reconnecting
	ws, _, err := websocket.DefaultDialer.Dial(websocketURL, http.Header{})
	if err == nil {
		err = ws.WriteJSON(initMessage)
	}
	// Keep trying on error
	for err != nil {
		log.Println(err)
		time.Sleep(5 * time.Second)
		ws, _, err = websocket.DefaultDialer.Dial(websocketURL, http.Header{})
		if err == nil {
			err = ws.WriteJSON(initMessage)
		}
	}

	log.Println("Successful reconnect")

	return ws
}

// Convert a wsResult to an exchange.Book
func (ok *OKCoin) convertToBook(respRef *response) *exchange.Book {
	resp := *respRef
	if resp[0].ErrorCode != 0 {
		return &exchange.Book{Error: fmt.Errorf("OKCoin book error code: %d", resp[0].ErrorCode)}
	}

	// Book structure from the exchange
	var tmp struct {
		Bids       [][2]float64 `json:"bids"`             // Slice of bid data items
		Asks       [][2]float64 `json:"asks"`             // Slice of ask data items
		Timestamp  int64        `json:"timestamp,string"` // Timestamp
		UnitAmount int          `json:"unit_amount"`      // Unit amount for futures
	}

	// Unmarshal response.Data into intermediate structure
	if err := json.Unmarshal(resp[0].Data, &tmp); err != nil {
		return &exchange.Book{Error: fmt.Errorf("OKCoin book error: %s", err.Error())}
	}

	// Translate into exchange.Book structure
	bids := make(exchange.BidItems, 20)
	asks := make(exchange.AskItems, 20)
	for i := 0; i < 20; i++ {
		bids[i].Price = tmp.Bids[i][0]
		bids[i].Amount = tmp.Bids[i][1]
		asks[i].Price = tmp.Asks[i][0]
		asks[i].Amount = tmp.Asks[i][1]
	}
	sort.Sort(bids)
	sort.Sort(asks)

	// Return book
	return &exchange.Book{
		Exg:   ok,
		Time:  time.Now(),
		Bids:  bids,
		Asks:  asks,
		Error: nil,
	}
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
	if err := json.Unmarshal(data, &response); err != nil {
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
	if err := json.Unmarshal(data, &response); err != nil {
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
	if err := json.Unmarshal(data, &response); err != nil {
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
