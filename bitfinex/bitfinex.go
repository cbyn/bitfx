// Bitfinex trading API

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

// URL for API
const URL = "https://api.bitfinex.com/v1/"

// Bitfinex exchange information
type Bitfinex struct {
	key, secret, symbol, currency, name           string
	priority                                      int
	position, fee, maxPos, availShort, availFunds float64
	currencyCode                                  byte
}

// New returns a pointer to a new Bitfinex instance
func New(key, secret, symbol, currency string, priority int, fee, availShort, availFunds float64) *Bitfinex {
	return &Bitfinex{
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
	}
}

// String implements the Stringer interface
func (bf *Bitfinex) String() string {
	return bf.name
}

// Priority returns the exchange priority for order execution
func (bf *Bitfinex) Priority() int {
	return bf.priority
}

// Fee returns the exchange order fee
func (bf *Bitfinex) Fee() float64 {
	return bf.fee
}

// SetPosition setter method
func (bf *Bitfinex) SetPosition(pos float64) {
	bf.position = pos
}

// Position getter method
func (bf *Bitfinex) Position() float64 {
	return bf.position
}

// Currency getter method
func (bf *Bitfinex) Currency() string {
	return bf.currency
}

// CurrencyCode getter method
func (bf *Bitfinex) CurrencyCode() byte {
	return bf.currencyCode
}

// SetMaxPos setter method
func (bf *Bitfinex) SetMaxPos(maxPos float64) {
	bf.maxPos = maxPos
}

// MaxPos getter method
func (bf *Bitfinex) MaxPos() float64 {
	return bf.maxPos
}

// AvailFunds getter method
func (bf *Bitfinex) AvailFunds() float64 {
	return bf.availFunds
}

// AvailShort getter method
func (bf *Bitfinex) AvailShort() float64 {
	return bf.availShort
}

// HasCrytpoFee returns true if fee is taken in cryptocurrency on buys
func (bf *Bitfinex) HasCryptoFee() bool {
	return false
}

// CommunicateBook sends the latest available book data on the supplied channel
func (bf *Bitfinex) CommunicateBook(bookChan chan<- exchange.Book, doneChan <-chan bool) exchange.Book {
	// Initial book to return
	book, _ := bf.getBook()

	// Run read loop in new goroutine
	go bf.runLoop(bookChan, doneChan)

	return book
}

// HTTP read loop
func (bf *Bitfinex) runLoop(bookChan chan<- exchange.Book, doneChan <-chan bool) {
	// Used to compare timestamps
	oldTimestamps := make([]float64, 40)

	for {
		select {
		case <-doneChan:
			return
		default:
			book, newTimestamps := bf.getBook()
			if bookChanged(oldTimestamps, newTimestamps) {
				bookChan <- book
			}
			oldTimestamps = newTimestamps
		}
	}
}

// Get book data with an http request
func (bf *Bitfinex) getBook() (exchange.Book, []float64) {
	// Used to compare timestamps
	timestamps := make([]float64, 40)

	// Send get request
	url := fmt.Sprintf("%sbook/%s%s?limit_bids=%d&limit_asks=%d", URL, bf.symbol, bf.currency, 20, 20)
	data, err := get(url)
	if err != nil {
		return exchange.Book{Error: fmt.Errorf("%s UpdateBook error: %s", bf, err.Error())}, timestamps
	}

	var tmp struct {
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

	if err := json.Unmarshal(data, &tmp); err != nil {
		return exchange.Book{Error: fmt.Errorf("%s UpdateBook error: %s", bf, err.Error())}, timestamps
	}

	bids := make(exchange.BidItems, 20)
	asks := make(exchange.AskItems, 20)
	for i := 0; i < 20; i++ {
		bids[i].Price = tmp.Bids[i].Price
		bids[i].Amount = tmp.Bids[i].Amount
		asks[i].Price = tmp.Asks[i].Price
		asks[i].Amount = tmp.Asks[i].Amount
		timestamps[i] = tmp.Bids[i].Timestamp
		timestamps[i+20] = tmp.Asks[i].Timestamp
	}

	sort.Sort(bids)
	sort.Sort(asks)

	// Return book
	return exchange.Book{
		Exg:   bf,
		Time:  time.Now(),
		Bids:  bids,
		Asks:  asks,
		Error: nil,
	}, timestamps
}

func bookChanged(timestamps1, timestamps2 []float64) bool {
	for i := 0; i < 40; i++ {
		if math.Abs(timestamps1[i]-timestamps2[i]) > .5 {
			return true
		}
	}
	return false
}

// SendOrder to the exchange
func (bf *Bitfinex) SendOrder(action, otype string, amount, price float64) (int64, error) {
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
		bf.symbol + bf.currency,
		amount,
		price,
		"bitfinex",
		action,
		otype,
	}

	// Send post request
	data, err := bf.post(URL+"order/new", request)
	if err != nil {
		return 0, fmt.Errorf("%s SendOrder error: %s", bf, err.Error())
	}

	// Unmarshal response
	var response struct {
		ID      int64  `json:"order_id"`
		Message string `json:"message"`
	}
	err = json.Unmarshal(data, &response)
	if err != nil {
		return 0, fmt.Errorf("%s SendOrder error: %s", bf, err.Error())
	}
	if response.Message != "" {
		return 0, fmt.Errorf("%s SendOrder error: %s", bf, response.Message)
	}

	return response.ID, nil
}

// CancelOrder on the exchange
func (bf *Bitfinex) CancelOrder(id int64) (bool, error) {
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

	// Send post request
	data, err := bf.post(URL+"order/cancel", request)
	if err != nil {
		return false, fmt.Errorf("%s CancelOrder error: %s", bf, err.Error())
	}

	// Unmarshal response
	var response struct {
		Message string `json:"message"`
	}
	err = json.Unmarshal(data, &response)
	if err != nil {
		return false, fmt.Errorf("%s CancelOrder error: %s", bf, err.Error())
	}
	if response.Message != "" {
		return false, fmt.Errorf("%s CancelOrder error: %s", bf, response.Message)
	}

	return true, nil
}

// GetOrderStatus of an order on the exchange
func (bf *Bitfinex) GetOrderStatus(id int64) (exchange.Order, error) {
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

	// Send post request
	data, err := bf.post(URL+"order/status", request)
	if err != nil {
		return order, fmt.Errorf("%s GetOrderStatus error: %s", bf, err.Error())
	}

	// Unmarshal response
	var response struct {
		Message        string  `json:"message"`
		IsLive         bool    `json:"is_live,bool"`
		ExecutedAmount float64 `json:"executed_amount,string"`
	}
	err = json.Unmarshal(data, &response)
	if err != nil {
		return order, fmt.Errorf("%s GetOrderStatus error: %s", bf, err.Error())
	}
	if response.Message != "" {
		return order, fmt.Errorf("%s GetOrderStatus error: %s", bf, response.Message)
	}

	if response.IsLive {
		order.Status = "live"
	} else {
		order.Status = "dead"
	}
	order.FilledAmount = math.Abs(response.ExecutedAmount)
	return order, nil
}

// authenticated POST
func (bf *Bitfinex) post(url string, payload interface{}) ([]byte, error) {
	// Payload = parameters-dictionary -> JSON encode -> base64
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return []byte{}, err
	}
	payloadBase64 := base64.StdEncoding.EncodeToString(payloadJSON)

	// Signature = HMAC-SHA384(payload, api-secret) as hexadecimal
	h := hmac.New(sha512.New384, []byte(bf.secret))
	h.Write([]byte(payloadBase64))
	signature := hex.EncodeToString(h.Sum(nil))

	client := &http.Client{}
	req, err := http.NewRequest("POST", url, nil)
	// req.Close = true
	if err != nil {
		return []byte{}, err
	}

	// HTTP headers:
	// X-BFX-APIKEY
	// X-BFX-PAYLOAD
	// X-BFX-SIGNATURE
	req.Header.Add("X-BFX-APIKEY", bf.key)
	req.Header.Add("X-BFX-PAYLOAD", payloadBase64)
	req.Header.Add("X-BFX-SIGNATURE", signature)

	resp, err := client.Do(req)
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
		return []byte{}, fmt.Errorf(resp.Status)
	}
	defer resp.Body.Close()

	return ioutil.ReadAll(resp.Body)
}
