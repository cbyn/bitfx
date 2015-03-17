// Forex data API

// http://finance.yahoo.com/webservice/v1/symbols/CNY=X/quote?format=json

package forex

import (
	"encoding/json"
	"fmt"
	// "github.com/davecgh/go-spew/spew"
	"io/ioutil"
	"net/http"
)

// Forex data API URL
const (
	DATAURL = "http://finance.yahoo.com/webservice/v1/symbols/"
)

// rawQuote returned for requested symbol
type rawQuote struct {
	List rawList `json:"list"`
}
type rawList struct {
	Resources []rawResources `json:"resources"`
}
type rawResources struct {
	Resource rawResource `json:"resource"`
}
type rawResource struct {
	Fields Quote `json:"fields"`
}

// Quote for requested symbol
type Quote struct {
	Name      string  `json:"name"`          // Name of instrument
	Price     float64 `json:"price,string"`  // Quote price
	Symbol    string  `json:"symbol"`        // Symbol for instrument
	Timestamp float64 `json:"ts,string"`     // Timestamp for quote
	Type      string  `json:"type"`          // Type of instrument
	UTC       string  `json:"utctime"`       // UTC time for quote
	Volume    float64 `json:"volume,string"` // Volume for quote
}

// GetQuote for requested instrument
func GetQuote(symbol string) (Quote, error) {
	var quote rawQuote

	url := fmt.Sprintf("%s/quote?format=json", symbol)

	data, err := get(url)
	if err != nil {
		return Quote{}, err
	}

	err = json.Unmarshal(data, &quote)
	if err != nil {
		return Quote{}, err
	}

	return quote.List.Resources[0].Resource.Fields, nil
}

// get API unauthenticated GET
func get(url string) ([]byte, error) {
	resp, err := http.Get(DATAURL + url)
	if err != nil {
		return []byte{}, err
	}
	defer resp.Body.Close()

	return ioutil.ReadAll(resp.Body)
}
