// Cryptocurrency arbitrage trading system

package main

import (
	"bitfx/bitfinex"
	"bitfx/exchange"
	"bitfx/okcoin"
	"code.google.com/p/gcfg"
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"strconv"
	"time"
)

// Config stores user configuration
type Config struct {
	Sec struct {
		Symbol      string  // Symbol to trade
		Currency    string  // Underlying currency
		Entries     int     // Number of book entries to request
		EntryArb    float64 // Min arb amount to enter a position
		ExitArb     float64 // Min arb amount to exit a position
		MaxPosition float64 // Max position size on any exchange
		MinNetPos   float64 // Min acceptable net position
		MinOrder    float64 // Min order size for arb trade
		MaxOrder    float64 // Max order size for arb trade
		PrintBook   bool    // Print book data?
	}
}

// Used for storing best markets across exchanges
type market struct {
	exg                          exchange.Exchange
	orderPrice, amount, adjPrice float64
}

// Global variables
var (
	logFile     os.File             // Log printed to file
	cfg         Config              // Configuration struct
	apiErrors   bool                // Set true on any errors
	exchanges   []exchange.Exchange // Slice of exchanges
	netPosition float64             // Net position accross exchanges
	pl          float64             // Net P&L for current run
)

func init() {
	fmt.Println("\nInitializing...")
	setConfig()
	setLog()
	setExchanges()
	setPositions()
	calcNetPosition()
}

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
	filename := fmt.Sprintf("%s_arb.log", cfg.Sec.Symbol)
	logFile, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
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

// Set positions from previous run if file exists
func setPositions() {
	filename := fmt.Sprintf("%s_positions.csv", cfg.Sec.Symbol)
	if file, err := os.Open(filename); err == nil {
		defer file.Close()
		reader := csv.NewReader(file)
		positions, err := reader.Read()
		if err != nil {
			log.Fatal(err)
		}
		for i, exg := range exchanges {
			position, err := strconv.ParseFloat(positions[i], 64)
			if err != nil {
				log.Fatal(err)
			}
			exg.SetPosition(position)
		}
		log.Printf("Loaded positions %v\n", positions)
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
	// Check for input and run main loop
	inputChan := make(chan rune)
	go checkStdin(inputChan)
	runMainLoop(inputChan)
}

// Check for any user input
func checkStdin(inputChan chan<- rune) {
	var ch rune
	fmt.Scanf("%c", &ch)
	inputChan <- ch
}

// Run loop until user input is received
func runMainLoop(inputChan <-chan rune) {
	var (
		doneChan = make(chan bool)
		start    time.Time
	)
	for {
		apiErrors = false
		// Record time for each iteration
		start = time.Now()
		// Exit gracefully if anything entered by user
		select {
		case <-inputChan:
			savePositions()
			closeLogFile()
			return
		default:
		}

		// Update book data in separate goroutines
		for _, exg := range exchanges {
			go updateBook(exg, doneChan)
		}
		// Block until all results are returned
		for _ = range exchanges {
			<-doneChan
		}
		if !apiErrors {
			bestBid, bestAsk := findBestMarket()
			checkArb(bestBid, bestAsk)
			printResults(bestBid, bestAsk, start)
		}
	}
}

// Update book for each exchange and notify when finished
func updateBook(exg exchange.Exchange, doneChan chan<- bool) {
	err := exg.UpdateBook(cfg.Sec.Entries)
	checkErr(err)

	doneChan <- true
}

// Find best bid and ask subject to MinOrder and MaxPosition
func findBestMarket() (market, market) {
	var bestBid, bestAsk market
	bestAsk.adjPrice = math.MaxFloat64 // Need to start with a high price
	// Loop through exchanges
	for _, exg := range exchanges {
		// If the exchange postition is not already max short
		ableToSell := math.Min(cfg.Sec.MaxPosition+exg.Position(), cfg.Sec.MaxOrder)
		if ableToSell >= cfg.Sec.MinOrder {
			// Loop through bids and aggregate amounts until required size
			var amount, aggPrice float64
			for _, bid := range exg.Book().Bids {
				// adjPrice is the amount-weighted average subject to ableToSell
				aggPrice += bid.Price * math.Min(ableToSell-amount, bid.Amount)
				amount += math.Min(ableToSell-amount, bid.Amount)
				if amount >= cfg.Sec.MinOrder {
					// Set as best bid if adjusted price is highest
					adjPrice := (aggPrice / amount) * (1 - exg.Fee())
					if adjPrice > bestBid.adjPrice {
						bestBid = market{exg, bid.Price, amount, adjPrice}
					}
					break
				}
			}
		}
		// If the exchange postition is not already max long
		ableToBuy := math.Min(cfg.Sec.MaxPosition-exg.Position(), cfg.Sec.MaxOrder)
		if ableToBuy >= cfg.Sec.MinOrder {
			// Loop through asks and aggregate amounts until required size
			var amount, aggPrice float64
			for _, ask := range exg.Book().Asks {
				// adjPrice is the amount-weighted average subject to ableToBuy
				aggPrice += ask.Price * math.Min(ableToBuy-amount, ask.Amount)
				amount += math.Min(ableToBuy-amount, ask.Amount)
				if amount >= cfg.Sec.MinOrder {
					// Set as best ask if adjusted price is lowest
					adjPrice := (aggPrice / amount) * (1 + exg.Fee())
					if adjPrice < bestAsk.adjPrice {
						bestAsk = market{exg, ask.Price, amount, adjPrice}
					}
					break
				}
			}
		}
	}

	return bestBid, bestAsk
}

// Send trade if arb exists or net position exists
func checkArb(bestBid, bestAsk market) {
	if netPosition >= cfg.Sec.MinNetPos {
		// If net long, exit where possible
		amount := math.Min(netPosition, bestBid.amount)
		log.Println("Net long position exit")
		fillChan := make(chan float64)
		go fillOrKill(bestBid.exg, "sell", amount, bestBid.orderPrice, fillChan)
		updatePL(bestBid.adjPrice, <-fillChan, "sell")
		calcNetPosition()
	} else if netPosition <= -cfg.Sec.MinNetPos {
		// If net short, exit where possible
		amount := math.Min(-netPosition, bestAsk.amount)
		log.Println("Net short position exit")
		fillChan := make(chan float64)
		go fillOrKill(bestAsk.exg, "buy", amount, bestAsk.orderPrice, fillChan)
		updatePL(bestAsk.adjPrice, <-fillChan, "buy")
		calcNetPosition()
	} else if bestBid.adjPrice-bestAsk.adjPrice >= cfg.Sec.EntryArb {
		// If arb exists, trade pair
		amount := math.Min(bestBid.amount, bestAsk.amount)
		log.Printf("*** Arb Opportunity: %.4f for %.2f on %s vs %s ***\n", bestBid.adjPrice-bestAsk.adjPrice, amount, bestBid.exg, bestAsk.exg)
		sendPair(bestBid, bestAsk, amount)
		calcNetPosition()
	} else if (bestBid.exg.Position() >= cfg.Sec.MinOrder && bestAsk.exg.Position() <= -cfg.Sec.MinOrder) &&
		(bestBid.adjPrice-bestAsk.adjPrice >= cfg.Sec.ExitArb) {
		// If inter-exchange position can be exited, trade pair
		available := math.Min(bestBid.amount, bestAsk.amount)
		needed := math.Min(bestBid.exg.Position(), -bestAsk.exg.Position())
		amount := math.Min(available, needed)
		log.Printf("*** Exit Opportunity: %.4f for %.2f on %s vs %s ***\n", bestBid.adjPrice-bestAsk.adjPrice, amount, bestBid.exg, bestAsk.exg)
		sendPair(bestBid, bestAsk, amount)
		calcNetPosition()
	}
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
		checkErr(err)
		if id != 0 {
			break
		}
	}
	// Check status and cancel if necessary
	for {
		order, err = exg.GetOrderStatus(id)
		checkErr(err)
		if order.Status == "live" {
			_, err = exg.CancelOrder(id)
			checkErr(err)
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
func printResults(bestBid, bestAsk market, start time.Time) {
	clearScreen()
	if cfg.Sec.PrintBook {
		printMarkets()
	}
	fmt.Println("   Positions:")
	fmt.Println("----------------")
	for _, exg := range exchanges {
		fmt.Printf("%-8s %7.2f\n", exg, exg.Position())
	}
	fmt.Println("----------------")
	fmt.Println("\nBest Market:")
	fmt.Printf("%v %.4f (%.4f) for %.2f / %.2f at %.4f (%.4f) %v\n",
		bestBid.exg, bestBid.orderPrice, bestBid.adjPrice, bestBid.amount, bestAsk.amount, bestAsk.orderPrice, bestAsk.adjPrice, bestAsk.exg)
	fmt.Println("\nOpportunity:")
	fmt.Printf("%.4f for %.2f\n", bestBid.adjPrice-bestAsk.adjPrice, math.Min(bestBid.amount, bestAsk.amount))
	fmt.Printf("\nRun P&L: $%.2f\n", pl)
	fmt.Printf("\n%v processing time...", time.Since(start))
}

// Print book data from each exchange
func printMarkets() {
	for _, exg := range exchanges {
		fmt.Printf("          %v\n", exg)
		fmt.Println("----------------------------")
		fmt.Printf("%-10s%-10s%8s\n", " Bid", "  Ask", "Size ")
		fmt.Println("----------------------------")
		for i := range exg.Book().Asks {
			item := exg.Book().Asks[len(exg.Book().Asks)-1-i]
			fmt.Printf("%-10s%-10.4f%8.2f\n", "", item.Price, item.Amount)
		}
		for _, item := range exg.Book().Bids {
			fmt.Printf("%-10.4f%-10.2s%8.2f\n", item.Price, "", item.Amount)
		}
		fmt.Println("----------------------------")
	}
}

// Clear the terminal between prints
func clearScreen() {
	c := exec.Command("clear")
	c.Stdout = os.Stdout
	c.Run()
}

// Called on any error
func checkErr(err error) {
	if err != nil {
		log.Println(err)
		apiErrors = true
	}
}

// Saves positions to file for next run
func savePositions() {
	filename := fmt.Sprintf("%s_positions.csv", cfg.Sec.Symbol)
	file, err := os.Create(filename)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	positions := make([]string, len(exchanges))
	for i, exg := range exchanges {
		positions[i] = fmt.Sprintf("%f", exg.Position())
	}
	writer := csv.NewWriter(file)
	err = writer.Write(positions)
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
