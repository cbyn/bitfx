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
	"time"
)

// Config stores user configuration
type Config struct {
	Sec struct {
		Symbol      string  // Symbol to trade
		Currency    string  // Underlying currency
		EntryArb    float64 // Min arb amount to enter a position
		ExitArb     float64 // Min arb amount to exit a position
		MaxPosition float64 // Max position size on any exchange
		MinNetPos   float64 // Min acceptable net position
		MinOrder    float64 // Min order size for arb trade
		MaxOrder    float64 // Max order size for arb trade
		PrintOn     bool    // Display results in terminal
	}
}

// Used for storing best markets across exchanges
type market struct {
	exg                          exchange.Exchange
	orderPrice, amount, adjPrice float64
}

// Used for syncronizing access to exchange book data
type readOp struct {
	exg  exchange.Exchange
	resp chan *exchange.Book
}

// Global variables
var (
	logFile     os.File             // Log printed to file
	cfg         Config              // Configuration struct
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
	fmt.Println("Running...")
	readChan := make(chan readOp)
	doneChan := make(chan bool)
	go handleBooks(readChan, doneChan)
	time.Sleep(5 * time.Second)

	for {
		bestBid, bestAsk := findBestMarket(readChan)
		checkArb(bestBid, bestAsk)
		if cfg.Sec.PrintOn {
			printResults(bestBid, bestAsk)
		}

		// Exit if anything entered by user
		select {
		case <-inputChan:
			doneChan <- true
			closeLogFile()
			fmt.Println("Terminated")
			return
		default:
		}
	}
}

// Handle book data from exchanges
func handleBooks(readChan <-chan readOp, doneChan <-chan bool) {
	// Map storing the current state
	books := make(map[exchange.Exchange]*exchange.Book)
	// Channel to receive book data from exchanges
	bookChan := make(chan *exchange.Book)
	// Channel to notify exchanges when done
	exgDoneChan := make(chan bool, len(exchanges))

	// Initiate communication with each exchange
	for _, exg := range exchanges {
		if err := exg.CommunicateBook(bookChan, exgDoneChan); err != nil {
			log.Fatal(err)
		}
		books[exg] = &exchange.Book{}
	}

	// Synchronize access to books (or finish if notified)
	for {
		select {
		case book := <-bookChan:
			if !isError(book.Error) {
				books[book.Exg] = book
			}
		case read := <-readChan:
			read.resp <- books[read.exg]
		case <-doneChan:
			for range exchanges {
				exgDoneChan <- true
			}
			return
		}
	}
}

// Find best bid and ask subject to MinOrder and MaxPosition
func findBestMarket(readChan chan<- readOp) (market, market) {
	var (
		bestBid, bestAsk market
		book             *exchange.Book
	)
	// Need to start with a high price for ask
	bestAsk.adjPrice = math.MaxFloat64
	// For book data requests
	read := readOp{resp: make(chan *exchange.Book)}

	// Loop through exchanges
	for _, exg := range exchanges {
		// Make book read request
		read.exg = exg
		readChan <- read
		book = <-read.resp

		ableToSell := math.Min(cfg.Sec.MaxPosition+exg.Position(), cfg.Sec.MaxOrder)
		// If the exchange postition is not already max short, and data is not old
		if ableToSell >= cfg.Sec.MinOrder && time.Since(book.Time) < 30*time.Second {
			// Loop through bids and aggregate amounts until required size
			var amount, aggPrice float64
			for _, bid := range book.Bids {
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
		ableToBuy := math.Min(cfg.Sec.MaxPosition-exg.Position(), cfg.Sec.MaxOrder)
		// If the exchange postition is not already max long, and data is not old
		if ableToBuy >= cfg.Sec.MinOrder && time.Since(book.Time) < 30*time.Second {
			// Loop through asks and aggregate amounts until required size
			var amount, aggPrice float64
			for _, ask := range book.Asks {
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
	if bestBid.exg == nil || bestAsk.exg == nil {
		return
	}

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
func printResults(bestBid, bestAsk market) {
	clearScreen()

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
