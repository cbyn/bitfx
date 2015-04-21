// OKCoin exchange API

package okcoin

import (
	"bitfx/exchange"
	"crypto/md5"
	"encoding/json"
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

// Client contains all exchange information
type Client struct {
	key, secret, symbol, currency, websocketURL, restURL, name string
	priority                                                   int
	position, fee, maxPos, availShort, availFunds              float64
	currencyCode                                               byte
	done                                                       chan bool
	writeBookMsg                                               chan request
	readBookMsg                                                chan response
	writeOrderMsg                                              chan request
	readOrderMsg                                               chan response
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

// New returns a pointer to a Client instance
func New(key, secret, symbol, currency string, priority int, fee, availShort, availFunds float64) *Client {
	// URL depends on currency
	var websocketURL, restURL string
	var currencyCode byte
	if strings.ToLower(currency) == "usd" {
		websocketURL = "wss://real.okcoin.com:10440/websocket/okcoinapi"
		restURL = "https://www.okcoin.com/api/v1"
		currencyCode = 0
	} else if strings.ToLower(currency) == "cny" {
		websocketURL = "wss://real.okcoin.cn:10440/websocket/okcoinapi"
		restURL = "https://www.okcoin.cn/api/v1"
		currencyCode = 1
	} else {
		log.Fatal("Currency must be USD or CNY")
	}
	name := fmt.Sprintf("OKCoin(%s)", currency)

	// Channels for WebSocket connections
	done := make(chan bool, 2)
	writeBookMsg := make(chan request)
	readBookMsg := make(chan response)
	writeOrderMsg := make(chan request)
	readOrderMsg := make(chan response)

	client := &Client{
		key:           key,
		secret:        secret,
		symbol:        symbol,
		currency:      currency,
		websocketURL:  websocketURL,
		restURL:       restURL,
		priority:      priority,
		fee:           fee,
		availShort:    availShort,
		availFunds:    availFunds,
		currencyCode:  currencyCode,
		name:          name,
		done:          done,
		writeOrderMsg: writeOrderMsg,
		readOrderMsg:  readOrderMsg,
		writeBookMsg:  writeBookMsg,
		readBookMsg:   readBookMsg,
	}

	// Run WebSocket connections
	initMsg := request{Event: "addChannel", Channel: fmt.Sprintf("ok_%s%s_depth", symbol, currency)}
	go client.maintainWS(initMsg, writeBookMsg, readBookMsg)
	go client.maintainWS(request{}, writeOrderMsg, readOrderMsg)

	return client
}

// Done closes all connections
func (client *Client) Done() {
	client.done <- true
	client.done <- true
	close(client.readBookMsg)
	close(client.readOrderMsg)
}

// String implements the Stringer interface
func (client *Client) String() string {
	return client.name
}

// Priority returns the exchange priority for order execution
func (client *Client) Priority() int {
	return client.priority
}

// Fee returns the exchange order fee
func (client *Client) Fee() float64 {
	return client.fee
}

// SetPosition sets the exchange position
func (client *Client) SetPosition(pos float64) {
	client.position = pos
}

// Position returns the exchange position
func (client *Client) Position() float64 {
	return client.position
}

// Currency returns the exchange currency
func (client *Client) Currency() string {
	return client.currency
}

// CurrencyCode returns the exchange currency code
func (client *Client) CurrencyCode() byte {
	return client.currencyCode
}

// SetMaxPos sets the exchange max position
func (client *Client) SetMaxPos(maxPos float64) {
	client.maxPos = maxPos
}

// MaxPos returns the exchange max position
func (client *Client) MaxPos() float64 {
	return client.maxPos
}

// AvailFunds returns the exchange available funds
func (client *Client) AvailFunds() float64 {
	return client.availFunds
}

// AvailShort returns the exchange quantity available for short selling
func (client *Client) AvailShort() float64 {
	return client.availShort
}

// HasCrytpoFee returns true if fee is taken in cryptocurrency on buys
func (client *Client) HasCryptoFee() bool {
	return true
}

// CommunicateBook sends the latest available book data on the supplied channel
func (client *Client) CommunicateBook(bookChan chan<- exchange.Book) exchange.Book {
	// Get an initial book to return
	book := client.convertToBook(<-client.readBookMsg)

	// Run a read loop in new goroutine
	go client.runBookLoop(bookChan)

	return book
}

// Websocket read loop
func (client *Client) runBookLoop(bookChan chan<- exchange.Book) {
	for resp := range client.readBookMsg {
		// Process data and send out to user
		bookChan <- client.convertToBook(resp)
	}
}

// Convert websocket data to an exchange.Book
func (client *Client) convertToBook(resp response) exchange.Book {
	// Unmarshal
	var bookData struct {
		Bids       [][2]float64 `json:"bids"`             // Slice of bid data items
		Asks       [][2]float64 `json:"asks"`             // Slice of ask data items
		Timestamp  int64        `json:"timestamp,string"` // Timestamp
		UnitAmount int          `json:"unit_amount"`      // Unit amount for futures

	}
	if err := json.Unmarshal(resp[0].Data, &bookData); err != nil {
		return exchange.Book{Error: fmt.Errorf("%s book error: %s", client, err)}
	}

	// Translate into exchange.Book structure
	bids := make(exchange.BidItems, 20)
	asks := make(exchange.AskItems, 20)
	for i := 0; i < 20; i++ {
		bids[i].Price = bookData.Bids[i][0]
		bids[i].Amount = bookData.Bids[i][1]
		asks[i].Price = bookData.Asks[i][0]
		asks[i].Amount = bookData.Asks[i][1]
	}
	sort.Sort(bids)
	sort.Sort(asks)

	// Return book
	return exchange.Book{
		Exg:   client,
		Time:  time.Now(),
		Bids:  bids,
		Asks:  asks,
		Error: nil,
	}
}

// SendOrder sends an order to the exchange
func (client *Client) SendOrder(action, otype string, amount, price float64) (int64, error) {
	// Construct parameters
	params := make(map[string]string)
	params["api_key"] = client.key
	params["symbol"] = fmt.Sprintf("%s_%s", client.symbol, client.currency)
	if otype == "limit" {
		params["type"] = action
	} else if otype == "market" {
		params["type"] = fmt.Sprintf("%s_%s", action, otype)
	}
	params["price"] = fmt.Sprintf("%f", price)
	params["amount"] = fmt.Sprintf("%f", amount)
	params["sign"] = client.constructSign(params)

	// Construct request
	channel := fmt.Sprintf("ok_spot%s_trade", client.currency)
	req := request{Event: "addChannel", Channel: channel, Parameters: params}

	// Write to WebSocket
	client.writeOrderMsg <- req

	// Read response
	resp := <-client.readOrderMsg

	if resp[0].ErrorCode != 0 {
		return 0, fmt.Errorf("%s SendOrder error code: %d", client, resp[0].ErrorCode)
	}

	// Unmarshal
	var orderData struct {
		ID     int64 `json:"order_id,string"`
		Result bool  `json:"result,string"`
	}
	if err := json.Unmarshal(resp[0].Data, &orderData); err != nil {
		return 0, fmt.Errorf("%s SendOrder error: %s", client, err)
	}
	if !orderData.Result {
		return 0, fmt.Errorf("%s SendOrder failure", client)
	}

	return orderData.ID, nil
}

// CancelOrder cancels an order on the exchange
func (client *Client) CancelOrder(id int64) (bool, error) {
	// Create parameter map for signing
	params := make(map[string]string)
	params["symbol"] = fmt.Sprintf("%s_%s", client.symbol, client.currency)
	params["order_id"] = fmt.Sprintf("%d", id)

	// Send POST request
	data, err := client.post(client.restURL+"/cancel_order.do", params)
	if err != nil {
		return false, fmt.Errorf("%s CancelOrder error: %s", client, err)
	}

	// Unmarshal response
	var response struct {
		Result    bool  `json:"result"`
		ErrorCode int64 `json:"error_code"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return false, fmt.Errorf("%s CancelOrder error: %s", client, err)
	}
	if response.ErrorCode != 0 {
		return false, fmt.Errorf("%s CancelOrder error code: %d", client, response.ErrorCode)
	}

	return response.Result, nil
}

// GetOrderStatus gets the status of an order on the exchange
func (client *Client) GetOrderStatus(id int64) (exchange.Order, error) {
	// Create parameter map for signing
	params := make(map[string]string)
	params["symbol"] = fmt.Sprintf("%s_%s", client.symbol, client.currency)
	params["order_id"] = fmt.Sprintf("%d", id)

	// Create order to be returned
	var order exchange.Order

	// Send POST request
	data, err := client.post(client.restURL+"/order_info.do", params)
	if err != nil {
		return order, fmt.Errorf("%s GetOrderStatus error: %s", client, err)
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
		return order, fmt.Errorf("%s GetOrderStatus error: %s", client, err)
	}
	if response.ErrorCode != 0 {
		return order, fmt.Errorf("%s GetOrderStatus error code: %d", client, response.ErrorCode)
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

// Construct sign for authentication
func (client *Client) constructSign(params map[string]string) string {
	// Make url.Values from params
	values := url.Values{}
	for param, value := range params {
		values.Set(param, value)
	}
	// Add authorization key to url.Values
	values.Set("api_key", client.key)
	// Prepare string to sign with MD5
	stringParams := values.Encode()
	// Add the authorization secret to the end
	stringParams += fmt.Sprintf("&secret_key=%s", client.secret)
	// Sign with MD5
	sum := md5.Sum([]byte(stringParams))

	return strings.ToUpper(fmt.Sprintf("%x", sum))
}

// Authenticated POST
func (client *Client) post(stringrestURL string, params map[string]string) ([]byte, error) {
	// Make url.Values from params
	values := url.Values{}
	for param, value := range params {
		values.Set(param, value)
	}
	// Add authorization key to url.Values
	values.Set("api_key", client.key)
	// Prepare string to sign with MD5
	stringParams := values.Encode()
	// Add the authorization secret to the end
	stringParams += fmt.Sprintf("&secret_key=%s", client.secret)
	// Sign with MD5
	sum := md5.Sum([]byte(stringParams))
	// Add sign to url.Values
	values.Set("sign", strings.ToUpper(fmt.Sprintf("%x", sum)))

	// Send POST
	resp, err := http.PostForm(stringrestURL, values)
	if err != nil {
		return []byte{}, err
	}
	if resp.StatusCode != 200 {
		return []byte{}, fmt.Errorf(resp.Status)
	}
	defer resp.Body.Close()

	return ioutil.ReadAll(resp.Body)

}

// Maintain a WebSocket connection
func (client *Client) maintainWS(initMsg request, writeMsg <-chan request, readMsg chan<- response) {
	// Get a WebSocket connection
	ws := client.persistentNewWS(initMsg)

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
				ws = client.persistentNewWS(initMsg)
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

	// Read from connection
	go func() {
		for {
			(<-receiveWS).SetReadDeadline(time.Now().Add(pingInterval + time.Second))
			_, data, err := (<-receiveWS).ReadMessage()
			if err != nil {
				// Reconnect on error
				log.Printf("%s WebSocket error: %s", client, err)
				reconnectWS <- true
			} else if string(data) != `{"event":"pong"}` {
				// Send out if not a pong and a receiver is ready
				var resp response
				if err := json.Unmarshal(data, &resp); err != nil {
					// Send response with error code on unmarshal errors
					resp = response{{ErrorCode: -2}}
				}
				select {
				case readMsg <- resp:
				default:
					// Discard data if a receiver is not ready
				}
			}
		}
	}()

	// Manage connection
	for {
		select {
		case <-client.done:
			// End if notified
			ticker.Stop()
			closeWS <- true
			return
		case <-ticker.C:
			// Send ping (true type-9 pings not supported by server)
			if err := (<-receiveWS).WriteMessage(1, ping); err != nil {
				// Reconnect on error
				log.Printf("%s WebSocket error: %s", client, err)
				reconnectWS <- true
			}
		case msg := <-writeMsg:
			// Write received message to WebSocket
			if err := (<-receiveWS).WriteJSON(msg); err != nil {
				// Notify sender and reconnect on error
				log.Printf("%s WebSocket error: %s", client, err)
				readMsg <- response{{ErrorCode: -1}}
				reconnectWS <- true
			}
		}
	}

}

// Get a new WebSocket connection subscribed to specified channel
func (client *Client) newWS(initMsg request) (*websocket.Conn, error) {
	// Get WebSocket connection
	ws, _, err := websocket.DefaultDialer.Dial(client.websocketURL, http.Header{})
	if err != nil {
		return nil, err
	}

	// Subscribe to channel if specified
	if initMsg.Event != "" {
		if err = ws.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
			return nil, err
		}
		if err = ws.WriteJSON(initMsg); err != nil {
			return nil, err
		}
	}

	// Set a zero timeout for future writes
	if err = ws.SetWriteDeadline(time.Time{}); err != nil {
		return nil, err
	}

	log.Println("Successful Connect")
	return ws, nil
}

// Connect WebSocket with repeated tries on failure
func (client *Client) persistentNewWS(initMsg request) *websocket.Conn {
	// Try connecting
	ws, err := client.newWS(initMsg)

	// Keep trying on error
	for err != nil {
		log.Printf("%s WebSocket error: %s", client, err)
		time.Sleep(1 * time.Second)
		ws, err = client.newWS(initMsg)
	}

	return ws
}
