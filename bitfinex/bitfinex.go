// Bitfinex exchange API

package bitfinex

import (
	"bitfx2/exchange"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"net/http"
	"sort"
	"strconv"
	"time"
)

// Client contains all exchange information
type Client struct {
	key, secret, symbol, currency, name, baseURL  string
	priority                                      int
	position, fee, maxPos, availShort, availFunds float64
	currencyCode                                  byte
}

// New returns a pointer to a Client instance
func New(key, secret, symbol, currency string, priority int, fee, availShort, availFunds float64) *Client {
	return &Client{
		key:          key,
		secret:       secret,
		symbol:       symbol,
		currency:     currency,
		priority:     priority,
		fee:          fee,
		availShort:   availShort,
		availFunds:   availFunds,
		currencyCode: 0,
		name:         fmt.Sprintf("Bitfinex(%s)", currency),
		baseURL:      "https://api.bitfinex.com",
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
	// Initial book to return
	book, _ := client.getBook()

	// Run read loop in new goroutine
	go client.runLoop(bookChan, doneChan)

	return book
}

// HTTP read loop
func (client *Client) runLoop(bookChan chan<- exchange.Book, doneChan <-chan bool) {
	// Used to compare timestamps
	oldTimestamps := make([]float64, 40)

	for {
		select {
		case <-doneChan:
			return
		default:
			book, newTimestamps := client.getBook()
			// Send out only if changed
			if bookChanged(oldTimestamps, newTimestamps) {
				bookChan <- book
			}
			oldTimestamps = newTimestamps
		}
	}
}

// Get book data with an HTTP request
func (client *Client) getBook() (exchange.Book, []float64) {
	// Used to compare timestamps
	timestamps := make([]float64, 40)

	// Send GET request
	url := fmt.Sprintf("%s/v1/book/%s%s?limit_bids=%d&limit_asks=%d", client.baseURL, client.symbol, client.currency, 20, 20)
	data, err := client.get(url)
	if err != nil {
		return exchange.Book{Error: fmt.Errorf("%s UpdateBook error: %s", client, err.Error())}, timestamps
	}

	// Unmarshal
	var response struct {
		Bids []struct {
			Price     float64 `json:"price,string"`
			Amount    float64 `json:"amount,string"`
			Timestamp float64 `json:"timestamp,string"`
		} `json:"bids"`
		Asks []struct {
			Price     float64 `json:"price,string"`
			Amount    float64 `json:"amount,string"`
			Timestamp float64 `json:"timestamp,string"`
		} `json:"asks"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return exchange.Book{Error: fmt.Errorf("%s UpdateBook error: %s", client, err.Error())}, timestamps
	}

	// Translate into an exchange.Book
	bids := make(exchange.BidItems, 20)
	asks := make(exchange.AskItems, 20)
	for i := 0; i < 20; i++ {
		bids[i].Price = response.Bids[i].Price
		bids[i].Amount = response.Bids[i].Amount
		asks[i].Price = response.Asks[i].Price
		asks[i].Amount = response.Asks[i].Amount
		timestamps[i] = response.Bids[i].Timestamp
		timestamps[i+20] = response.Asks[i].Timestamp
	}
	sort.Sort(bids)
	sort.Sort(asks)

	// Return book and timestamps
	return exchange.Book{
		Exg:   client,
		Time:  time.Now(),
		Bids:  bids,
		Asks:  asks,
		Error: nil,
	}, timestamps
}

// Returns true if the book has changed
func bookChanged(timestamps1, timestamps2 []float64) bool {
	for i := 0; i < 40; i++ {
		if math.Abs(timestamps1[i]-timestamps2[i]) > .5 {
			return true
		}
	}
	return false
}

// SendOrder sends an order to the exchange
func (client *Client) SendOrder(action, otype string, amount, price float64) (int64, error) {
	// Create request struct
	request := struct {
		URL      string  `json:"request"`
		Nonce    string  `json:"nonce"`
		Symbol   string  `json:"symbol"`
		Amount   float64 `json:"amount,string"`
		Price    float64 `json:"price,string"`
		Exchange string  `json:"exchange"`
		Side     string  `json:"side"`
		Type     string  `json:"type"`
	}{
		"/v1/order/new",
		strconv.FormatInt(time.Now().UnixNano(), 10),
		client.symbol + client.currency,
		amount,
		price,
		"bitfinex",
		action,
		otype,
	}

	// Send POST request
	data, err := client.post(client.baseURL+request.URL, request)
	if err != nil {
		return 0, fmt.Errorf("%s SendOrder error: %s", client, err.Error())
	}

	// Unmarshal response
	var response struct {
		ID      int64  `json:"order_id"`
		Message string `json:"message"`
	}
	err = json.Unmarshal(data, &response)
	if err != nil {
		return 0, fmt.Errorf("%s SendOrder error: %s", client, err.Error())
	}
	if response.Message != "" {
		return 0, fmt.Errorf("%s SendOrder error: %s", client, response.Message)
	}

	return response.ID, nil
}

// CancelOrder cancels an order on the exchange
func (client *Client) CancelOrder(id int64) (bool, error) {
	// Create request struct
	request := struct {
		URL     string `json:"request"`
		Nonce   string `json:"nonce"`
		OrderID int64  `json:"order_id"`
	}{
		"/v1/order/cancel",
		strconv.FormatInt(time.Now().UnixNano(), 10),
		id,
	}

	// Send POST request
	data, err := client.post(client.baseURL+request.URL, request)
	if err != nil {
		return false, fmt.Errorf("%s CancelOrder error: %s", client, err.Error())
	}

	// Unmarshal response
	var response struct {
		Message string `json:"message"`
	}
	err = json.Unmarshal(data, &response)
	if err != nil {
		return false, fmt.Errorf("%s CancelOrder error: %s", client, err.Error())
	}
	if response.Message != "" {
		return false, fmt.Errorf("%s CancelOrder error: %s", client, response.Message)
	}

	return true, nil
}

// GetOrderStatus gets the status of an order on the exchange
func (client *Client) GetOrderStatus(id int64) (exchange.Order, error) {
	// Create request struct
	request := struct {
		URL     string `json:"request"`
		Nonce   string `json:"nonce"`
		OrderID int64  `json:"order_id"`
	}{
		"/v1/order/status",
		strconv.FormatInt(time.Now().UnixNano(), 10),
		id,
	}

	// Create order to be returned
	var order exchange.Order

	// Send POST request
	data, err := client.post(client.baseURL+request.URL, request)
	if err != nil {
		return order, fmt.Errorf("%s GetOrderStatus error: %s", client, err.Error())
	}

	// Unmarshal response
	var response struct {
		Message        string  `json:"message"`
		IsLive         bool    `json:"is_live,bool"`
		ExecutedAmount float64 `json:"executed_amount,string"`
	}
	err = json.Unmarshal(data, &response)
	if err != nil {
		return order, fmt.Errorf("%s GetOrderStatus error: %s", client, err.Error())
	}
	if response.Message != "" {
		return order, fmt.Errorf("%s GetOrderStatus error: %s", client, response.Message)
	}

	if response.IsLive {
		order.Status = "live"
	} else {
		order.Status = "dead"
	}
	order.FilledAmount = math.Abs(response.ExecutedAmount)
	return order, nil
}

// Authenticated POST
func (client *Client) post(url string, payload interface{}) ([]byte, error) {
	// Payload = parameters-dictionary -> JSON encode -> base64
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return []byte{}, err
	}
	payloadBase64 := base64.StdEncoding.EncodeToString(payloadJSON)

	// Signature = HMAC-SHA384(payload, api-secret) as hexadecimal
	h := hmac.New(sha512.New384, []byte(client.secret))
	h.Write([]byte(payloadBase64))
	signature := hex.EncodeToString(h.Sum(nil))

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return []byte{}, err
	}

	// HTTP headers:
	// X-BFX-APIKEY
	// X-BFX-PAYLOAD
	// X-BFX-SIGNATURE
	req.Header.Add("X-BFX-APIKEY", client.key)
	req.Header.Add("X-BFX-PAYLOAD", payloadBase64)
	req.Header.Add("X-BFX-SIGNATURE", signature)

	httpClient := http.Client{}
	resp, err := httpClient.Do(req)
	if err != nil {
		return []byte{}, err
	}
	defer resp.Body.Close()

	return ioutil.ReadAll(resp.Body)
}

// Unauthenticated GET
func (client *Client) get(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return []byte{}, err
	}
	if resp.StatusCode != 200 {
		return []byte{}, fmt.Errorf(resp.Status)
	}
	defer resp.Body.Close()

	return ioutil.ReadAll(resp.Body)
}
