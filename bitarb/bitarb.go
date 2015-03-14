// Cryptocurrency arbitrage trading system

package main

import (
	"bitfx2/bitfinex"
	"bitfx2/exchange"
	"bitfx2/okcoin"
	"code.google.com/p/gcfg"
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"strconv"
)

// Config stores user configuration
type Config struct {
	Sec struct {
		Symbol      string  // Symbol to trade
		Currency    string  // Underlying currency
		MaxArb      float64 // Top limit for position entry
		MinArb      float64 // Bottom limit for position exit
		MaxPosition float64 // Max position size on any exchange
		MinNetPos   float64 // Min acceptable net position
		MinOrder    float64 // Min order size for arb trade
		MaxOrder    float64 // Max order size for arb trade
		PrintOn     bool    // Display results in terminal
	}
}

// Used for filtered book data
type filteredBook struct {
	bid, ask market
}
type market struct {
	exg                          exchange.Exchange
	orderPrice, amount, adjPrice float64
}

// Global variables
var (
	logFile     os.File             // Log printed to file
	cfg         Config              // Configuration struct
	exchanges   []exchange.Exchange // Slice of exchanges
	netPosition float64             // Net position accross exchanges
	pl          float64             // Net P&L for current run
)

// Set config info
func setConfig() {
	configFile := flag.String("config", "bitarb.gcfg", "Configuration file")
	flag.Parse()
	err := gcfg.ReadFileInto(&cfg, *configFile)
	if err != nil {
		log.Fatal(err)
	}
}

// Set file for logging
func setLog() {
	logFile, err := os.OpenFile("bitarb.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatal(err)
	}
	log.SetOutput(logFile)
	log.Println("Starting new run")
}

// Initialize exchanges
func setExchanges() {
	exchanges = []exchange.Exchange{
		bitfinex.New(os.Getenv("BITFINEX_KEY"), os.Getenv("BITFINEX_SECRET"), cfg.Sec.Symbol, cfg.Sec.Currency, 2, 0.001),
		okcoin.New(os.Getenv("OKCOIN_KEY"), os.Getenv("OKCOIN_SECRET"), cfg.Sec.Symbol, cfg.Sec.Currency, 1, 0.002),
	}
	for _, exg := range exchanges {
		log.Printf("Using exchange %s with priority %d and fee of %.4f", exg, exg.Priority(), exg.Fee())
	}
}

// Set status from previous run if file exists
func setStatus() {
	if file, err := os.Open("status.csv"); err == nil {
		defer file.Close()
		reader := csv.NewReader(file)
		status, err := reader.Read()
		if err != nil {
			log.Fatal(err)
		}
		for i, exg := range exchanges {
			position, err := strconv.ParseFloat(status[i], 64)
			if err != nil {
				log.Fatal(err)
			}
			exg.SetPosition(position)
		}
		pl, err = strconv.ParseFloat(status[len(status)-1], 64)
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("Loaded positions %v\n", status[0:len(status)-1])
		log.Printf("Loaded P&L %f\n", pl)
	}
}

// Calculate total position across exchanges
func calcNetPosition() {
	netPosition = 0
	for _, exg := range exchanges {
		netPosition += exg.Position()
	}
}

func main() {
	fmt.Println("Running...")

	setConfig()
	setLog()
	setExchanges()
	setStatus()
	calcNetPosition()

	// For communicating best markets
	marketChan := make(chan map[exchange.Exchange]filteredBook)
	// For notifying termination (on any user input)
	doneChan := make(chan bool)

	// Launch goroutines
	go checkStdin(doneChan)
	// go considerTrade(marketChan)

	// Main data loop
	handleData(marketChan, doneChan)

	// Finish
	saveStatus()
	closeLogFile()
	fmt.Println("~~~ Fini ~~~")
}

// Check for any user input
func checkStdin(doneChan chan<- bool) {
	var ch rune
	fmt.Scanf("%c", &ch)
	doneChan <- true
}

// Handle book data from exchanges
func handleData(marketChan chan<- map[exchange.Exchange]filteredBook, doneChan <-chan bool) {
	// Current relevant market data for each exchange
	markets := make(map[exchange.Exchange]filteredBook)
	// Channel to receive book data from exchanges
	bookChan := make(chan *exchange.Book)
	// Channel to notify exchanges when finished
	exgDoneChan := make(chan bool, len(exchanges))

	// Initiate communication with each exchange
	for _, exg := range exchanges {
		if err := exg.CommunicateBook(bookChan, exgDoneChan); err != nil {
			log.Fatal(err)
		}
		markets[exg] = filteredBook{}
	}

	for {
		select {
		// Receive data from an exchange
		case book := <-bookChan:
			if !isError(book.Error) {
				markets[book.Exg] = filterBook(book)
				// Send if ready to be used, otherwise discard
				select {
				case marketChan <- markets:
				default:
				}
			}
		// Finish
		case <-doneChan:
			for range exchanges {
				exgDoneChan <- true
			}
			close(marketChan)
			return
		}
	}
}

// Filter book data down to tradable market
// Adjusts market amounts according to MaxOrder
func filterBook(bookRef *exchange.Book) filteredBook {
	book := *bookRef
	var fb filteredBook

	// Loop through bids and aggregate amounts until required size
	var amount, aggPrice float64
	for _, bid := range book.Bids {
		// adjPrice is the amount-weighted average subject to MaxOrder
		aggPrice += bid.Price * math.Min(cfg.Sec.MaxOrder-amount, bid.Amount)
		amount += math.Min(cfg.Sec.MaxOrder-amount, bid.Amount)
		if amount >= cfg.Sec.MinOrder {
			adjPrice := (aggPrice / amount) * (1 - book.Exg.Fee())
			fb.bid = market{book.Exg, bid.Price, amount, adjPrice}
			break
		}
	}

	// Loop through asks and aggregate amounts until required size
	amount, aggPrice = 0, 0
	for _, ask := range book.Asks {
		// adjPrice is the amount-weighted average subject to MaxOrder
		aggPrice += ask.Price * math.Min(cfg.Sec.MaxOrder-amount, ask.Amount)
		amount += math.Min(cfg.Sec.MaxOrder-amount, ask.Amount)
		if amount >= cfg.Sec.MinOrder {
			adjPrice := (aggPrice / amount) * (1 + book.Exg.Fee())
			fb.ask = market{book.Exg, ask.Price, amount, adjPrice}
			break
		}
	}

	return fb
}

// TODO: Check that data is not old?

// Trade if net position exists or arb exists
func considerTrade(marketChan <-chan map[exchange.Exchange]filteredBook) {
	for markets := range marketChan {
		if netPosition >= cfg.Sec.MinNetPos {
			// If net long, find best bid and sell
			bestBid := findBestBid(markets)
			amount := math.Min(netPosition, bestBid.amount)
			fillChan := make(chan float64)
			log.Println("***** Net long position exit *****")
			go fillOrKill(bestBid.exg, "sell", amount, bestBid.orderPrice, fillChan)
			updatePL(bestBid.adjPrice, <-fillChan, "sell")
			calcNetPosition()
			if cfg.Sec.PrintOn {
				printResults()
			}
		} else if netPosition <= -cfg.Sec.MinNetPos {
			// If net short, exit where possible
			bestAsk := findBestAsk(markets)
			amount := math.Min(-netPosition, bestAsk.amount)
			fillChan := make(chan float64)
			log.Println("***** Net short position exit *****")
			go fillOrKill(bestAsk.exg, "buy", amount, bestAsk.orderPrice, fillChan)
			updatePL(bestAsk.adjPrice, <-fillChan, "buy")
			calcNetPosition()
			if cfg.Sec.PrintOn {
				printResults()
			}
		} else {
			// Check for arb opportunities
			if bestBid, bestAsk, exists := findBestArb(markets); exists {
				amount := math.Min(bestBid.amount, bestAsk.amount)
				log.Printf("***** Arb Opportunity: %.4f for %.2f on %s vs %s *****\n", bestBid.adjPrice-bestAsk.adjPrice, amount, bestBid.exg, bestAsk.exg)
				sendPair(bestBid, bestAsk, amount)
				calcNetPosition()
				if cfg.Sec.PrintOn {
					printResults()
				}
			}
		}
	}
}

// Find best bid able to sell
// Adjusts market amount according to exchange position
func findBestBid(markets map[exchange.Exchange]filteredBook) market {
	var bestBid market

	for exg, fb := range markets {
		ableToSell := exg.Position() + cfg.Sec.MaxPosition
		// If not already max short
		if ableToSell >= cfg.Sec.MinOrder {
			// If highest bid
			if fb.bid.adjPrice > bestBid.adjPrice {
				bestBid = fb.bid
				bestBid.amount = math.Min(bestBid.amount, ableToSell)
			}
		}
	}

	return bestBid
}

// Find best ask able to buy
// Adjusts market amount according to exchange position
func findBestAsk(markets map[exchange.Exchange]filteredBook) market {
	var bestAsk market
	// Need to start with a high number
	bestAsk.adjPrice = math.MaxFloat64

	for exg, fb := range markets {
		ableToBuy := cfg.Sec.MaxPosition - exg.Position()
		// If not already max long
		if ableToBuy >= cfg.Sec.MinOrder {
			// If lowest ask
			if fb.ask.adjPrice < bestAsk.adjPrice {
				bestAsk = fb.ask
				bestAsk.amount = math.Min(bestAsk.amount, ableToBuy)
			}
		}
	}

	return bestAsk

}

// Find best arbitrage opportunity
// Adjusts market amounts according to exchange positions
func findBestArb(markets map[exchange.Exchange]filteredBook) (market, market, bool) {
	var bestBid, bestAsk market
	bestOpp := 0.0
	exists := false

	// Compare each exchange bid to all other asks
	for exg1, fb1 := range markets {
		ableToSell := exg1.Position() + cfg.Sec.MaxPosition
		// If exg1 is not already max short
		if ableToSell >= cfg.Sec.MinOrder {
			for exg2, fb2 := range markets {
				ableToBuy := cfg.Sec.MaxPosition - exg2.Position()
				// If exg2 is not already max long
				if ableToBuy >= cfg.Sec.MinOrder {
					opp := fb1.bid.adjPrice - fb2.ask.adjPrice - calcNeededArb(exg2.Position(), exg1.Position())
					if opp > bestOpp {
						bestBid = fb1.bid
						bestBid.amount = math.Min(bestBid.amount, ableToSell)
						bestAsk = fb2.ask
						bestAsk.amount = math.Min(bestAsk.amount, ableToBuy)
						exists = true
						bestOpp = opp
					}
				}
			}
		}
	}

	return bestBid, bestAsk, exists
}

// Calculate arb needed for a trade based on existing positions
func calcNeededArb(buyExgPos, sellExgPos float64) float64 {
	// Middle between user-defined min and max
	center := (cfg.Sec.MaxArb + cfg.Sec.MinArb) / 2
	// Half distance from center to min and max
	halfDist := (cfg.Sec.MaxArb - center) / 2
	// Percent of max allowed position for each
	buyExgPct := buyExgPos / cfg.Sec.MaxPosition
	sellExgPct := sellExgPos / cfg.Sec.MaxPosition

	return center + buyExgPct*halfDist - sellExgPct*halfDist
}

// Logic for sending a pair of orders
func sendPair(bestBid, bestAsk market, amount float64) {
	fillChan1 := make(chan float64)
	fillChan2 := make(chan float64)
	if bestBid.exg.Priority() == bestAsk.exg.Priority() {
		// If exchanges have equal priority, send simultaneous orders
		go fillOrKill(bestAsk.exg, "buy", amount, bestAsk.orderPrice, fillChan1)
		go fillOrKill(bestBid.exg, "sell", amount, bestBid.orderPrice, fillChan2)
		updatePL(bestAsk.adjPrice, <-fillChan1, "buy")
		updatePL(bestBid.adjPrice, <-fillChan2, "sell")
	} else if bestBid.exg.Priority() < bestAsk.exg.Priority() {
		// If bestBid exchange has priority, confirm fill before sending other side
		go fillOrKill(bestBid.exg, "sell", amount, bestBid.orderPrice, fillChan2)
		amount = <-fillChan2
		updatePL(bestBid.adjPrice, amount, "sell")
		if amount >= cfg.Sec.MinNetPos {
			go fillOrKill(bestAsk.exg, "buy", amount, bestAsk.orderPrice, fillChan1)
			updatePL(bestAsk.adjPrice, <-fillChan1, "buy")
		}
	} else {
		// Reverse priority
		go fillOrKill(bestAsk.exg, "buy", amount, bestAsk.orderPrice, fillChan1)
		amount = <-fillChan1
		updatePL(bestAsk.adjPrice, amount, "buy")
		if amount >= cfg.Sec.MinNetPos {
			go fillOrKill(bestBid.exg, "sell", amount, bestBid.orderPrice, fillChan2)
			updatePL(bestBid.adjPrice, <-fillChan2, "sell")
		}
	}
}

// Update P&L
func updatePL(price, amount float64, action string) {
	if action == "buy" {
		amount = -amount
	}
	pl += price * amount
}

// Handle communication for a FOK order
func fillOrKill(exg exchange.Exchange, action string, amount, price float64, fillChan chan<- float64) {
	var (
		id    int64
		err   error
		order exchange.Order
	)
	// Send order
	for {
		id, err = exg.SendOrder(action, "limit", amount, price)
		isError(err)
		if id != 0 {
			break
		}
	}
	// Check status and cancel if necessary
	for {
		order, err = exg.GetOrderStatus(id)
		isError(err)
		if order.Status == "live" {
			_, err = exg.CancelOrder(id)
			isError(err)
		} else if order.Status == "dead" {
			break
		}
		// Continues while order status is empty
	}
	// Update position
	if action == "buy" {
		position := exg.Position() + order.FilledAmount
		exg.SetPosition(position)
	} else {
		position := exg.Position() - order.FilledAmount
		exg.SetPosition(position)
	}
	// Print to log
	log.Printf("%s trade: %s %.2f at %.4f\n", exg, action, order.FilledAmount, price)

	fillChan <- order.FilledAmount
}

// Print relevant data to terminal
func printResults() {
	clearScreen()

	fmt.Println("   Positions:")
	fmt.Println("----------------")
	for _, exg := range exchanges {
		fmt.Printf("%-8s %7.2f\n", exg, exg.Position())
	}
	// fmt.Println("----------------")
	fmt.Printf("\nRun P&L: $%.2f\n", pl)
}

// Clear the terminal between prints
func clearScreen() {
	c := exec.Command("clear")
	c.Stdout = os.Stdout
	c.Run()
}

// Called on any error
func isError(err error) bool {
	if err != nil {
		log.Println(err)
		return true
	}
	return false
}

// Save status to file
func saveStatus() {
	file, err := os.Create("status.csv")
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	status := make([]string, len(exchanges)+1)
	for i, exg := range exchanges {
		status[i] = fmt.Sprintf("%f", exg.Position())
	}
	status[len(exchanges)] = fmt.Sprintf("%f", pl)
	writer := csv.NewWriter(file)
	err = writer.Write(status)
	if err != nil {
		log.Fatal(err)
	}
	writer.Flush()
}

// Close log file on exit
func closeLogFile() {
	log.Println("Ending run")
	logFile.Close()
}
