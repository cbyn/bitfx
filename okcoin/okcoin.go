// OKCoin trading API

package okcoin

import (
	"bitfx2/exchange"
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// OKCoin exchange information
type OKCoin struct {
	key, secret, symbol, currency, websocketURL, restURL, name string
	priority                                                   int
	position, fee, maxPos                                      float64
	currencyCode                                               byte
}

// Exchange request format
type request struct {
	Event      string            `json:"event"`      // Event to request
	Channel    string            `json:"channel"`    // Channel on which to make request
	Parameters map[string]string `json:"parameters"` // Additional parameters
}

// New returns a pointer to a new OKCoin instance
func New(key, secret, symbol, currency string, priority int, fee, maxPos float64) *OKCoin {
	// URL depends on currency
	var websocketURL, restURL string
	var currencyCode byte
	if strings.ToLower(currency) == "usd" {
		websocketURL = "wss://real.okcoin.com:10440/websocket/okcoinapi"
		restURL = "https://www.okcoin.com/api/v1/"
		currencyCode = 0
	} else if strings.ToLower(currency) == "cny" {
		websocketURL = "wss://real.okcoin.cn:10440/websocket/okcoinapi"
		restURL = "https://www.okcoin.cn/api/v1/"
		currencyCode = 1
	} else {
		log.Fatal("Currency must be USD or CNY")
	}

	return &OKCoin{
		key:          key,
		secret:       secret,
		symbol:       symbol,
		currency:     currency,
		websocketURL: websocketURL,
		restURL:      restURL,
		priority:     priority,
		fee:          fee,
		maxPos:       maxPos,
		currencyCode: currencyCode,
		name:         fmt.Sprintf("OKCoin(%s)", currency),
	}
}

// String implements the Stringer interface
func (ok *OKCoin) String() string {
	return ok.name
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

// Currency getter method
func (ok *OKCoin) Currency() string {
	return ok.currency
}

// CurrencyCode getter method
func (ok *OKCoin) CurrencyCode() byte {
	return ok.currencyCode
}

// MaxPos getter method
func (ok *OKCoin) MaxPos() float64 {
	return ok.maxPos
}

// CommunicateBook sends the latest available book data on the supplied channel
func (ok *OKCoin) CommunicateBook(bookChan chan<- exchange.Book, doneChan <-chan bool) exchange.Book {
	// Connect to websocket
	ws, _, err := websocket.DefaultDialer.Dial(ok.websocketURL, http.Header{})
	if err != nil {
		return exchange.Book{Error: fmt.Errorf("%s CommunicateBook error: %s", ok, err)}
	}

	// Send request for book data
	channel := fmt.Sprintf("ok_%s%s_depth", ok.symbol, ok.currency)
	initMessage := request{Event: "addChannel", Channel: channel}
	if err = ws.WriteJSON(initMessage); err != nil {
		return exchange.Book{Error: fmt.Errorf("%s CommunicateBook error: %s", ok, err)}
	}
	// Get an initial book to return
	_, data, err := ws.ReadMessage()
	if err != nil {
		return exchange.Book{Error: fmt.Errorf("%s CommunicateBook error: %s", ok, err)}
	}
	book := ok.convertToBook(data)

	// Run a read loop in new goroutine
	go ok.runLoop(ws, initMessage, bookChan, doneChan)

	return book
}

// Websocket read loop
func (ok *OKCoin) runLoop(ws *websocket.Conn, initMessage request, bookChan chan<- exchange.Book, doneChan <-chan bool) {
	// Syncronize access to *websocket.Conn
	receiveWS := make(chan *websocket.Conn)
	reconnectWS := make(chan bool)
	closeWS := make(chan bool)
	go func() {
	LOOP:
		for {
			select {
			// Request to use websocket
			case receiveWS <- ws:
			// Request to reconnect websocket
			case <-reconnectWS:
				ws.Close()
				ws = ok.reconnect(initMessage)
			// Request to close websocket
			case <-closeWS:
				ws.Close()
				break LOOP
			}
		}
	}()

	// Setup heartbeat
	pingInterval := 15 * time.Second
	ticker := time.NewTicker(pingInterval)
	ping := []byte(`{"event":"ping"}`)

	// Read from websocket
	dataChan := make(chan []byte)
	go func() {
		for {
			(<-receiveWS).SetReadDeadline(time.Now().Add(pingInterval + time.Second))
			_, data, err := (<-receiveWS).ReadMessage()
			if err != nil {
				// Reconnect on error
				log.Printf("%s WebSocket error: %s", ok, err)
				reconnectWS <- true
			} else if string(data) != `{"event":"pong"}` {
				// If not a pong, send for processing
				dataChan <- data
			}
		}
	}()

	for {
		select {
		case <-doneChan:
			// End if notified
			ticker.Stop()
			closeWS <- true
			return
		case <-ticker.C:
			// Send ping (true type-9 pings not supported by server)
			if err := (<-receiveWS).WriteMessage(1, ping); err != nil {
				// Reconnect on error
				log.Printf("%s WebSocket error: %s", ok, err)
				reconnectWS <- true
			}
		case data := <-dataChan:
			// Process data and send out to user
			bookChan <- ok.convertToBook(data)
		}
	}
}

// Reconnect websocket
func (ok *OKCoin) reconnect(initMessage request) *websocket.Conn {
	log.Println("Reconnecting...")

	// Try reconnecting
	ws, _, err := websocket.DefaultDialer.Dial(ok.websocketURL, http.Header{})
	if err == nil {
		err = ws.WriteJSON(initMessage)
	}
	// Keep trying on error
	for err != nil {
		log.Printf("%s WebSocket error: %s", ok, err)
		time.Sleep(1 * time.Second)
		ws, _, err = websocket.DefaultDialer.Dial(ok.websocketURL, http.Header{})
		if err == nil {
			err = ws.WriteJSON(initMessage)
		}
	}

	log.Println("Successful reconnect")

	return ws
}

// Convert websocket data to an exchange.Book
func (ok *OKCoin) convertToBook(data []byte) exchange.Book {
	// Unmarshal
	var resp []struct {
		Channel   string `json:"channel"`          // Channel name
		ErrorCode int64  `json:"errorcode,string"` // Error code if not successful
		Data      struct {
			Bids       [][2]float64 `json:"bids"`             // Slice of bid data items
			Asks       [][2]float64 `json:"asks"`             // Slice of ask data items
			Timestamp  int64        `json:"timestamp,string"` // Timestamp
			UnitAmount int          `json:"unit_amount"`      // Unit amount for futures

		} `json:"data"` // Data specific to channel
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return exchange.Book{Error: fmt.Errorf("%s book error: %s", ok, err)}
	}

	// Return error if there is an exchange error code
	if resp[0].ErrorCode != 0 {
		return exchange.Book{Error: fmt.Errorf("%s book error code: %d", ok, resp[0].ErrorCode)}
	}

	// Translate into exchange.Book structure
	bids := make(exchange.BidItems, 20)
	asks := make(exchange.AskItems, 20)
	for i := 0; i < 20; i++ {
		bids[i].Price = resp[0].Data.Bids[i][0]
		bids[i].Amount = resp[0].Data.Bids[i][1]
		asks[i].Price = resp[0].Data.Asks[i][0]
		asks[i].Amount = resp[0].Data.Asks[i][1]
	}
	sort.Sort(bids)
	sort.Sort(asks)

	// Return book
	return exchange.Book{
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
	data, err := ok.post(ok.restURL+"trade.do", params)
	if err != nil {
		return 0, fmt.Errorf("%s SendOrder error: %s", ok, err)
	}

	// Unmarshal response
	var response struct {
		ID        int64 `json:"order_id"`
		ErrorCode int64 `json:"error_code"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return 0, fmt.Errorf("%s SendOrder error: %s", ok, err)
	}
	if response.ErrorCode != 0 {
		return 0, fmt.Errorf("%s SendOrder error code: %d", ok, response.ErrorCode)
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
	data, err := ok.post(ok.restURL+"cancel_order.do", params)
	if err != nil {
		return false, fmt.Errorf("%s CancelOrder error: %s", ok, err)
	}

	// Unmarshal response
	var response struct {
		Result    bool  `json:"result"`
		ErrorCode int64 `json:"error_code"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return false, fmt.Errorf("%s CancelOrder error: %s", ok, err)
	}
	if response.ErrorCode != 0 {
		return false, fmt.Errorf("%s CancelOrder error code: %d", ok, response.ErrorCode)
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
	data, err := ok.post(ok.restURL+"order_info.do", params)
	if err != nil {
		return order, fmt.Errorf("%s GetOrderStatus error: %s", ok, err)
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
		return order, fmt.Errorf("%s GetOrderStatus error: %s", ok, err)
	}
	if response.ErrorCode != 0 {
		return order, fmt.Errorf("%s GetOrderStatus error code: %d", ok, response.ErrorCode)
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
