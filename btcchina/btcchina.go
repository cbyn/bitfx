// BTCChina exchange API

package btcchina

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
}

// Exchange request format
type request struct {
	Event      string            `json:"event"`      // Event to request
	Channel    string            `json:"channel"`    // Channel on which to make request
	Parameters map[string]string `json:"parameters"` // Additional parameters
}

// New returns a pointer to a Client instance
func New(key, secret, symbol, currency string, priority int, fee, availShort, availFunds float64) *Client {
	return &Client{
		key:          key,
		secret:       secret,
		symbol:       symbol,
		currency:     currency,
		websocketURL: "websocket.btcchina.com/socket.io",
		restURL:      "api.btcchina.com",
		priority:     priority,
		fee:          fee,
		availShort:   availShort,
		availFunds:   availFunds,
		currencyCode: 1,
		name:         fmt.Sprintf("BTCChina(%s)", currency),
	}
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
	return false
}

// CommunicateBook sends the latest available book data on the supplied channel
func (client *Client) CommunicateBook(bookChan chan<- exchange.Book, doneChan <-chan bool) exchange.Book {
	// Connect to Socket.IO
	ws, pingInterval, err := client.connectSocketIO()
	if err != nil {
		return exchange.Book{Error: fmt.Errorf("%s CommunicateBook error: %s", client, err)}
	}

	// Get an initial book to return
	_, data, err := ws.ReadMessage()
	if err != nil {
		return exchange.Book{Error: fmt.Errorf("%s CommunicateBook error: %s", client, err)}
	}
	book := client.convertToBook(data)

	// Run a read loop in new goroutine
	go client.runLoop(ws, pingInterval, bookChan, doneChan)

	return book
}

// Connect to Socket.IO
func (client *Client) connectSocketIO() (*websocket.Conn, time.Duration, error) {
	// Socket.IO handshake
	getURL := fmt.Sprintf("https://%s/?transport=polling", client.websocketURL)
	resp, err := http.Get(getURL)
	if err != nil {
		return nil, time.Duration(0), err
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, time.Duration(0), err
	}
	resp.Body.Close()
	message := strings.TrimLeftFunc(string(body), func(char rune) bool { return string(char) != "{" })
	var session struct {
		Sid          string
		Upgrades     []string
		PingInterval int
		PingTimeout  int
	}
	if err := json.Unmarshal([]byte(message), &session); err != nil {
		return nil, time.Duration(0), err
	}
	for _, value := range session.Upgrades {
		if strings.ToLower(value) == "websocket" {
			break
		}
		return nil, time.Duration(0), fmt.Errorf("WebSocket upgrade not available")
	}
	wsURL := fmt.Sprintf("wss://%s/?transport=websocket&sid=%s", client.websocketURL, session.Sid)
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, http.Header{})
	if err != nil {
		return nil, time.Duration(0), err
	}

	// Upgrade connection to WebSocket
	if err := ws.WriteMessage(1, []byte("52")); err != nil {
		return nil, time.Duration(0), err
	}
	_, data, err := ws.ReadMessage()
	if err != nil {
		return nil, time.Duration(0), err
	}
	if string(data) != "40" {
		return nil, time.Duration(0), fmt.Errorf("Failed WebSocket upgrade")
	}

	// Subscribe to channel
	subMsg := fmt.Sprintf("42[\"subscribe\",\"grouporder_%s%s\"]", client.currency, client.symbol)
	if err := ws.WriteMessage(1, []byte(subMsg)); err != nil {
		return nil, time.Duration(0), err
	}

	// Return WebSocket and ping interval
	return ws, time.Duration(session.PingInterval) * time.Millisecond, nil
}

// Websocket read loop
func (client *Client) runLoop(ws *websocket.Conn, pingInterval time.Duration, bookChan chan<- exchange.Book, doneChan <-chan bool) {
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
				var err error
				ws, _, err = client.connectSocketIO()
				// Keep trying on error
				for err != nil {
					log.Printf("%s WebSocket error: %s", client, err)
					time.Sleep(1 * time.Second)
					ws, _, err = client.connectSocketIO()
				}
			// Request to close websocket
			case <-closeWS:
				ws.Close()
				break LOOP
			}
		}
	}()

	// Read from websocket
	dataChan := make(chan []byte)
	go func() {
		for {
			(<-receiveWS).SetReadDeadline(time.Now().Add(pingInterval + time.Second))
			_, data, err := (<-receiveWS).ReadMessage()
			if err != nil {
				// Reconnect on error
				log.Printf("%s WebSocket error: %s", client, err)
				reconnectWS <- true
			} else if string(data) != "3" {
				// If not a pong, send for processing
				dataChan <- data
			}
		}
	}()

	// Setup heartbeat
	ticker := time.NewTicker(pingInterval)
	ping := []byte("2")

	for {
		select {
		case <-doneChan:
			// End if notified
			ticker.Stop()
			closeWS <- true
			return
		case <-ticker.C:
			// Send Socket.IO ping
			if err := (<-receiveWS).WriteMessage(1, ping); err != nil {
				// Reconnect on error
				log.Printf("%s WebSocket error: %s", client, err)
				reconnectWS <- true
			}
		case data := <-dataChan:
			// Process data and send out to user
			bookChan <- client.convertToBook(data)
		}
	}
}

// Convert websocket data to an exchange.Book
func (client *Client) convertToBook(data []byte) exchange.Book {
	// Remove Socket.IO crap
	message := strings.TrimLeftFunc(string(data), func(char rune) bool { return string(char) != "{" })
	message = strings.TrimRightFunc(message, func(char rune) bool { return string(char) != "}" })
	// Unmarshal
	var response struct {
		GroupOrder struct {
			Bid []struct {
				Price       float64
				TotalAmount float64
			}
			Ask []struct {
				Price       float64
				TotalAmount float64
			}
		}
	}
	if err := json.Unmarshal([]byte(message), &response); err != nil {
		return exchange.Book{Error: fmt.Errorf("%s book error: %s", client, err)}
	}

	// Translate into exchange.Book structure
	bids := make(exchange.BidItems, 5)
	asks := make(exchange.AskItems, 5)
	// Only depth of 5 is available
	for i := 0; i < 5; i++ {
		bids[i].Price = response.GroupOrder.Bid[i].Price
		bids[i].Amount = response.GroupOrder.Bid[i].TotalAmount
		asks[i].Price = response.GroupOrder.Ask[i].Price
		asks[i].Amount = response.GroupOrder.Ask[i].TotalAmount
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
	// Create parameter map for signing
	params := make(map[string]string)
	params["symbol"] = fmt.Sprintf("%s_%s", client.symbol, client.currency)
	if otype == "limit" {
		params["type"] = action
	} else if otype == "market" {
		params["type"] = fmt.Sprintf("%s_%s", action, otype)
	}
	params["price"] = fmt.Sprintf("%f", price)
	params["amount"] = fmt.Sprintf("%f", amount)

	// Send POST request
	data, err := client.post(client.restURL+"/trade.do", params)
	if err != nil {
		return 0, fmt.Errorf("%s SendOrder error: %s", client, err)
	}

	// Unmarshal response
	var response struct {
		ID        int64 `json:"order_id"`
		ErrorCode int64 `json:"error_code"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return 0, fmt.Errorf("%s SendOrder error: %s", client, err)
	}
	if response.ErrorCode != 0 {
		return 0, fmt.Errorf("%s SendOrder error code: %d", client, response.ErrorCode)
	}

	return response.ID, nil
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
