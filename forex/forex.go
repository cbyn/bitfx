// Forex data API
// Currently using yahoo finance
// http://finance.yahoo.com/webservice/v1/symbols/CNY=X/quote?format=json

package forex

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"
)

// Forex data API URL
const DATAURL = "http://finance.yahoo.com/webservice/v1/symbols/"

// Quote contains forex quote information
type Quote struct {
	Price  float64
	Symbol string
	Error  error
}

// CommunicateFX sends the latest FX quote to the supplied channel
func CommunicateFX(symbol string, fxChan chan<- Quote, doneChan <-chan bool) Quote {
	// Initial quote to return
	quote := getQuote(symbol)

	// Run read loop in new goroutine
	go runLoop(symbol, fxChan, doneChan)

	return quote
}

// HTTP read loop
func runLoop(symbol string, fxChan chan<- Quote, doneChan <-chan bool) {
	ticker := time.NewTicker(15 * time.Second)

	for {
		select {
		case <-doneChan:
			ticker.Stop()
			return
		case <-ticker.C:
			fxChan <- getQuote(symbol)
		}
	}
}

// Returns quote for requested currency
func getQuote(symbol string) Quote {
	// Get data
	url := fmt.Sprintf("%s%s=x/quote?format=json", DATAURL, symbol)
	data, err := get(url)
	if err != nil {
		return Quote{Error: fmt.Errorf("Forex error %s", err)}
	}

	// Unmarshal
	response := struct {
		List struct {
			Resources []struct {
				Resource struct {
					Fields struct {
						Price float64 `json:"price,string"`
					} `json:"fields"`
				} `json:"resource"`
			} `json:"resources"`
		} `json:"list"`
	}{}
	if err = json.Unmarshal(data, &response); err != nil {
		return Quote{Error: fmt.Errorf("Forex error %s", err)}
	}

	// Pull out price
	price := response.List.Resources[0].Resource.Fields.Price
	if price < .000001 {
		return Quote{Error: fmt.Errorf("Forex zero price error")}
	}

	return Quote{
		Price:  price,
		Symbol: symbol,
		Error:  nil,
	}
}

// Unauthenticated GET
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
