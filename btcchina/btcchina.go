// BTCChina exchange API

package btcchina

import (
	"bitfx/exchange"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"sort"
	"strconv"
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
		restURL:      "api.btcchina.com/api_trade_v1.php",
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
	return 0, nil
}

// CancelOrder cancels an order on the exchange
func (client *Client) CancelOrder(id int64) (bool, error) {
	return true, nil
}

// GetOrderStatus gets the status of an order on the exchange
func (client *Client) GetOrderStatus(id int64) (exchange.Order, error) {
	return exchange.Order{}, nil
}

// Authenticated POST
func (client *Client) post(payload string, tonce int64) ([]byte, error) {
	// Perform HMAC on payload using secret
	h := hmac.New(sha1.New, []byte(client.secret))
	h.Write([]byte(payload))

	// Create http request using specified url
	url := fmt.Sprintf("https://%s:%x@%s", client.key, h.Sum(nil), client.restURL)
	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return []byte{}, err
	}

	// Add tonce header
	req.Header.Add("Json-Rpc-Tonce", strconv.FormatInt(tonce, 10))

	// Send POST
	httpClient := http.Client{}
	resp, err := httpClient.Do(req)
	if err != nil {
		return []byte{}, err
	}
	if resp.StatusCode != 200 {
		return []byte{}, fmt.Errorf(resp.Status)
	}
	defer resp.Body.Close()

	return ioutil.ReadAll(resp.Body)
}
