// OKCoin trading API

package okcoin

import (
	"bitfx/exchange"
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// URL for API
const URL = "https://www.okcoin.com/api/v1/"

// OKCoin exchange information
type OKCoin struct {
	key, secret, symbol, currency string
	priority                      int
	position, fee                 float64
	book                          exchange.Book
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

// Book getter method
func (ok *OKCoin) Book() exchange.Book {
	return ok.book
}

// UpdateBook updates the cached order book
func (ok *OKCoin) UpdateBook(entries int) error {
	var book exchange.Book

	url := fmt.Sprintf("%sdepth.do?symbol=%s_%s&size=%d", URL, ok.symbol, ok.currency, entries)
	data, err := get(url)
	if err != nil {
		return errors.New("OKCoin UpdateBook error: " + err.Error())
	}

	var tmp struct {
		Bids [][2]float64 `json:"bids"`
		Asks [][2]float64 `json:"asks"`
	}

	err = json.Unmarshal(data, &tmp)
	if err != nil {
		return err
	}

	book.Bids = make(exchange.BidItems, entries, entries)
	book.Asks = make(exchange.AskItems, entries, entries)
	for i := 0; i < entries; i++ {
		book.Bids[i].Price = tmp.Bids[i][0]
		book.Bids[i].Amount = tmp.Bids[i][1]
		book.Asks[i].Price = tmp.Asks[i][0]
		book.Asks[i].Amount = tmp.Asks[i][1]
	}

	sort.Sort(book.Bids)
	sort.Sort(book.Asks)

	ok.book = book

	return nil
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
	data, err := ok.post(URL+"trade.do", params)
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
	data, err := ok.post(URL+"cancel_order.do", params)
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
	data, err := ok.post(URL+"order_info.do", params)
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

func (ok *OKCoin) post(stringURL string, params map[string]string) ([]byte, error) {
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
	resp, err := http.PostForm(stringURL, values)
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
