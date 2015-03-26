// Cryptocurrency arbitrage trading system

// TODO:
// Set maxPos relative to price?
// - maxShort = maxPos, maxBuyPower = available currency
// Quit bitarb on repeated errors?
// Use yahoo and openexchange for FX?
// Use arb logic for best bid and ask?
// Use websocket for orders
// Auto margining on okcoin

package main

import (
	"bitfx2/bitfinex"
	"bitfx2/exchange"
	"bitfx2/forex"
	"bitfx2/okcoin"
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"strconv"
	"time"

	"code.google.com/p/gcfg"
)

// Config stores user configuration
type Config struct {
	Sec struct {
		Symbol         string  // Symbol to trade
		MaxArb         float64 // Top limit for position entry
		MinArb         float64 // Bottom limit for position exit
		FXPremium      float64 // Amount added to arb for taking FX risk
		MaxPosBitfinex float64 // Max position size
		MaxPosOkUSD    float64 // Max position size
		MaxPosOkCNY    float64 // Max position size
		MinNetPos      float64 // Min acceptable net position
		MinOrder       float64 // Min order size for arb trade
		MaxOrder       float64 // Max order size for arb trade
		PrintOn        bool    // Display results in terminal
	}
}

// Used for filtered book data
type filteredBook struct {
	bid, ask market
	time     time.Time
}
type market struct {
	exg                          exchange.Exchange
	orderPrice, amount, adjPrice float64
}

// Global variables
var (
	logFile     os.File             // Log printed to file
	cfg         Config              // Configuration struct
	exchanges   []exchange.Exchange // Slice of exchanges in use
	currencies  []string            // Slice of forein currencies in use
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
		bitfinex.New(os.Getenv("BITFINEX_KEY"), os.Getenv("BITFINEX_SECRET"), cfg.Sec.Symbol, "usd", 2, 0.001, cfg.Sec.MaxPosBitfinex),
		okcoin.New(os.Getenv("OKUSD_KEY"), os.Getenv("OKUSD_SECRET"), cfg.Sec.Symbol, "usd", 1, 0.002, cfg.Sec.MaxPosOkUSD),
		okcoin.New(os.Getenv("OKCNY_KEY"), os.Getenv("OKCNY_SECRET"), cfg.Sec.Symbol, "cny", 3, 0.000, cfg.Sec.MaxPosOkCNY),
	}
	for _, exg := range exchanges {
		log.Printf("Using exchange %s with priority %d and fee of %.4f", exg, exg.Priority(), exg.Fee())
	}
	currencies = append(currencies, "cny")
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
		log.Printf("%s Position: %.2f\n", exg, exg.Position())
	}
}

func main() {
	fmt.Println("Running...")

	// Initialization
	setConfig()
	setLog()
	setExchanges()
	setStatus()
	calcNetPosition()

	// Terminate on user input
	doneChan := make(chan bool, 1)
	go checkStdin(doneChan)

	// Communicate data
	requestBook := make(chan exchange.Exchange)
	receiveBook := make(chan filteredBook)
	newBook := make(chan bool)
	go handleData(requestBook, receiveBook, newBook, doneChan)

	// Check for opportunities
	considerTrade(requestBook, receiveBook, newBook)

	// Finish
	saveStatus()
	closeLogFile()
	fmt.Println("~~~ Fini ~~~")
}

// Check for user input
func checkStdin(doneChan chan<- bool) {
	var ch rune
	fmt.Scanf("%c", &ch)
	doneChan <- true
}

// Handle all data communication
func handleData(requestBook <-chan exchange.Exchange, receiveBook chan<- filteredBook, newBook chan<- bool, doneChan <-chan bool) {
	// Communicate forex
	requestFX := make(chan string)
	receiveFX := make(chan float64)
	fxDoneChan := make(chan bool, 1)
	go handleFX(requestFX, receiveFX, fxDoneChan)

	// Filtered book data for each exchange
	markets := make(map[exchange.Exchange]filteredBook)
	// Channel to receive book data from exchanges
	bookChan := make(chan exchange.Book)
	// Channel to notify exchanges when finished
	exgDoneChan := make(chan bool, len(exchanges))

	// Initiate communication with each exchange and initialize markets map
	for _, exg := range exchanges {
		book := exg.CommunicateBook(bookChan, exgDoneChan)
		if book.Error != nil {
			log.Fatal(book.Error)
		}
		requestFX <- exg.Currency()
		markets[exg] = filterBook(book, <-receiveFX)
	}

	// Handle data until notified of termination
	for {
		select {
		// Incoming data from an exchange
		case book := <-bookChan:
			if !isError(book.Error) {
				requestFX <- book.Exg.Currency()
				markets[book.Exg] = filterBook(book, <-receiveFX)
				// Notify of new data if receiver is not busy
				select {
				case newBook <- true:
				default:
				}
			}
		// New request for data
		case exg := <-requestBook:
			receiveBook <- markets[exg]
		// Termination
		case <-doneChan:
			close(newBook)
			fxDoneChan <- true
			for _ = range exchanges {
				exgDoneChan <- true
			}
			return
		}
	}
}

// Handle FX quotes
func handleFX(requestFX <-chan string, receiveFX chan<- float64, doneChan <-chan bool) {
	prices := make(map[string]float64)
	prices["usd"] = 1
	fxChan := make(chan forex.Quote)
	fxDoneChan := make(chan bool)
	// Initiate communication and initialize prices map
	for _, symbol := range currencies {
		quote := forex.CommunicateFX(symbol, fxChan, fxDoneChan)
		if quote.Error != nil {
			log.Fatal(quote.Error)
		}
		prices[symbol] = quote.Price
	}

	// Handle data until notified of termination
	for {
		select {
		// Incoming forex quote
		case quote := <-fxChan:
			if !isError(quote.Error) {
				prices[quote.Symbol] = quote.Price
			}
		// New request for price
		case symbol := <-requestFX:
			receiveFX <- prices[symbol]
		// Termination
		case <-doneChan:
			fxDoneChan <- true
			return
		}
	}
}

// Filter book down to relevant data for trading decisions
// Adjusts market amounts according to MaxOrder
func filterBook(book exchange.Book, fxPrice float64) filteredBook {
	fb := filteredBook{time: book.Time}
	// Loop through bids and aggregate amounts until required size
	var amount, aggPrice float64
	for _, bid := range book.Bids {
		aggPrice += bid.Price * math.Min(cfg.Sec.MaxOrder-amount, bid.Amount)
		amount += math.Min(cfg.Sec.MaxOrder-amount, bid.Amount)
		if amount >= cfg.Sec.MinOrder {
			// Amount-weighted average subject to MaxOrder, adjusted for fees and currency
			adjPrice := (aggPrice / amount) * (1 - book.Exg.Fee()) / fxPrice
			fb.bid = market{book.Exg, bid.Price, amount, adjPrice}
			break
		}
	}

	// Loop through asks and aggregate amounts until required size
	amount, aggPrice = 0, 0
	for _, ask := range book.Asks {
		aggPrice += ask.Price * math.Min(cfg.Sec.MaxOrder-amount, ask.Amount)
		amount += math.Min(cfg.Sec.MaxOrder-amount, ask.Amount)
		if amount >= cfg.Sec.MinOrder {
			// Amount-weighted average subject to MaxOrder, adjusted for fees and currency
			adjPrice := (aggPrice / amount) * (1 + book.Exg.Fee()) / fxPrice
			fb.ask = market{book.Exg, ask.Price, amount, adjPrice}
			break
		}
	}

	return fb
}

// Trade on net position exits and arb opportunities
func considerTrade(requestBook chan<- exchange.Exchange, receiveBook <-chan filteredBook, newBook <-chan bool) {
	// Local data copy
	var markets map[exchange.Exchange]filteredBook
	// For tracking last trade, to prevent false repeats on slow exchange updates
	var lastArb, lastAmount float64

	// Check for trade whenever new data is available
	for _ = range newBook {
		// Build local snapshot of latest data
		markets = make(map[exchange.Exchange]filteredBook)
		for _, exg := range exchanges {
			requestBook <- exg
			// Don't use potentially stale data
			if fb := <-receiveBook; time.Since(fb.time) < time.Minute {
				markets[exg] = fb
			}
		}
		// If net long from a previous missed leg, hit best bid
		if netPosition >= cfg.Sec.MinNetPos {
			bestBid := findBestBid(markets)
			amount := math.Min(netPosition, bestBid.amount)
			fillChan := make(chan float64)
			log.Println("NET LONG POSITION EXIT")
			go fillOrKill(bestBid.exg, "sell", amount, bestBid.orderPrice, fillChan)
			updatePL(bestBid.adjPrice, <-fillChan, "sell")
			calcNetPosition()
			if cfg.Sec.PrintOn {
				printResults()
			}
			// Else if net short, lift best ask
		} else if netPosition <= -cfg.Sec.MinNetPos {
			bestAsk := findBestAsk(markets)
			amount := math.Min(-netPosition, bestAsk.amount)
			fillChan := make(chan float64)
			log.Println("NET SHORT POSITION EXIT")
			go fillOrKill(bestAsk.exg, "buy", amount, bestAsk.orderPrice, fillChan)
			updatePL(bestAsk.adjPrice, <-fillChan, "buy")
			calcNetPosition()
			if cfg.Sec.PrintOn {
				printResults()
			}
			// Else check for arb opportunities
		} else {
			// If an opportunity exists
			if bestBid, bestAsk, exists := findBestArb(markets); exists {
				arb := bestBid.adjPrice - bestAsk.adjPrice
				amount := math.Min(bestBid.amount, bestAsk.amount)

				// If it's not a false repeat, then trade
				if math.Abs(arb-lastArb) > .000001 || math.Abs(amount-lastAmount) > .000001 || math.Abs(amount-cfg.Sec.MaxOrder) < .000001 {
					log.Printf("***** Arb Opportunity: %.4f for %.4f on %s vs %s *****\n", arb, amount, bestAsk.exg, bestBid.exg)
					sendPair(bestBid, bestAsk, amount)
					calcNetPosition()
					if cfg.Sec.PrintOn {
						printResults()
					}
					lastArb = arb
					lastAmount = amount
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
		ableToSell := exg.Position() + exg.MaxPos()
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
		ableToBuy := exg.MaxPos() - exg.Position()
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
	var (
		bestBid, bestAsk market
		bestOpp          float64
		exists           bool
	)

	// Compare each bid to all other asks
	for exg1, fb1 := range markets {
		ableToSell := exg1.Position() + exg1.MaxPos()
		// If exg1 is not already max short
		if ableToSell >= cfg.Sec.MinOrder {
			for exg2, fb2 := range markets {
				ableToBuy := exg2.MaxPos() - exg2.Position()
				// If exg2 is not already max long
				if ableToBuy >= cfg.Sec.MinOrder {
					opp := fb1.bid.adjPrice - fb2.ask.adjPrice - calcNeededArb(exg2, exg1)
					// If best opportunity
					if opp >= bestOpp {
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
func calcNeededArb(buyExg, sellExg exchange.Exchange) float64 {
	// Middle between min and max
	center := (cfg.Sec.MaxArb + cfg.Sec.MinArb) / 2
	// Half distance from center to min and max
	halfDist := (cfg.Sec.MaxArb - center) / 2
	// If taking currency risk, add required premium
	if buyExg.CurrencyCode() != sellExg.CurrencyCode() {
		center += cfg.Sec.FXPremium
	}
	// Percent of max
	buyExgPct := buyExg.Position() / buyExg.MaxPos()
	sellExgPct := sellExg.Position() / sellExg.MaxPos()

	// Return required arb
	return center + buyExgPct*halfDist - sellExgPct*halfDist
}

// Logic for sending a pair of orders
func sendPair(bestBid, bestAsk market, amount float64) {
	fillChan1 := make(chan float64)
	fillChan2 := make(chan float64)
	// If exchanges have equal priority, send simultaneous orders
	if bestBid.exg.Priority() == bestAsk.exg.Priority() {
		go fillOrKill(bestAsk.exg, "buy", amount, bestAsk.orderPrice, fillChan1)
		go fillOrKill(bestBid.exg, "sell", amount, bestBid.orderPrice, fillChan2)
		updatePL(bestAsk.adjPrice, <-fillChan1, "buy")
		updatePL(bestBid.adjPrice, <-fillChan2, "sell")
		// Else if bestBid exchange has priority, confirm fill before sending other side
	} else if bestBid.exg.Priority() < bestAsk.exg.Priority() {
		go fillOrKill(bestBid.exg, "sell", amount, bestBid.orderPrice, fillChan2)
		amount = <-fillChan2
		updatePL(bestBid.adjPrice, amount, "sell")
		if amount >= cfg.Sec.MinNetPos {
			go fillOrKill(bestAsk.exg, "buy", amount, bestAsk.orderPrice, fillChan1)
			updatePL(bestAsk.adjPrice, <-fillChan1, "buy")
		}
		// Else reverse priority
	} else {
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
		// Continues while order status is non-empty
	}

	filledAmount := order.FilledAmount

	// Update position
	if action == "buy" {
		if exg.HasCryptoFee() {
			filledAmount = filledAmount * (1 - exg.Fee())
		}
		exg.SetPosition(exg.Position() + filledAmount)
	} else {
		exg.SetPosition(exg.Position() - filledAmount)
	}
	// Print to log
	log.Printf("%s trade: %s %.4f at %.4f\n", exg, action, order.FilledAmount, price)

	fillChan <- filledAmount
}

// Print relevant data to terminal
func printResults() {
	clearScreen()

	fmt.Println("        Positions:")
	fmt.Println("--------------------------")
	for _, exg := range exchanges {
		fmt.Printf("%-13s %10.2f\n", exg, exg.Position())
	}
	fmt.Println("--------------------------")
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
