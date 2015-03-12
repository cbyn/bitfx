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

// Used for storing best market across exchanges
type bestMarket struct {
	bid market
	ask market
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
	// For communicating best markets
	marketChan := make(chan bestMarket)
	// For notifying termination (on any user input)
	doneChan := make(chan bool)

	// Launch goroutines
	go checkStdin(doneChan)
	go considerTrade(marketChan)
	handleData(marketChan, doneChan)
	// Fini
	savePositions()
	closeLogFile()
}

// Check for any user input
func checkStdin(doneChan chan<- bool) {
	var ch rune
	fmt.Scanf("%c", &ch)
	doneChan <- true
}

// Handle book data from exchanges
func handleData(marketChan chan<- bestMarket, doneChan <-chan bool) {
	// Map storing current book data for each exchange
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

	for {
		select {
		// Receive data from an exchange
		case book := <-bookChan:
			if !isError(book.Error) {
				books[book.Exg] = book
				// Send if ready to be used, otherwise discard
				select {
				case marketChan <- findBestMarket(books):
				default:
				}
			}
		// Fini
		case <-doneChan:
			for range exchanges {
				exgDoneChan <- true
			}
			close(marketChan)
			return
		}
	}
}

// Find best bid and ask subject to MinOrder and MaxPosition
func findBestMarket(books map[exchange.Exchange]*exchange.Book) bestMarket {
	var (
		best bestMarket
		book exchange.Book
	)
	// Need to start with a high price for ask
	best.ask.adjPrice = math.MaxFloat64

	// Loop through exchanges
	for _, exg := range exchanges {
		book = *books[exg]
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
					if adjPrice > best.bid.adjPrice {
						best.bid = market{exg, bid.Price, amount, adjPrice}
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
					if adjPrice < best.ask.adjPrice {
						best.ask = market{exg, ask.Price, amount, adjPrice}
					}
					break
				}
			}
		}
	}

	return best
}

// Send trade if arb exists or net position exists
func considerTrade(marketChan <-chan bestMarket) {
	for best := range marketChan {
		if best.bid.exg == nil || best.ask.exg == nil {
			continue
		}

		if netPosition >= cfg.Sec.MinNetPos {
			// If net long, exit where possible
			amount := math.Min(netPosition, best.bid.amount)
			log.Println("Net long position exit")
			fillChan := make(chan float64)
			go fillOrKill(best.bid.exg, "sell", amount, best.bid.orderPrice, fillChan)
			updatePL(best.bid.adjPrice, <-fillChan, "sell")
			calcNetPosition()
		} else if netPosition <= -cfg.Sec.MinNetPos {
			// If net short, exit where possible
			amount := math.Min(-netPosition, best.ask.amount)
			log.Println("Net short position exit")
			fillChan := make(chan float64)
			go fillOrKill(best.ask.exg, "buy", amount, best.ask.orderPrice, fillChan)
			updatePL(best.ask.adjPrice, <-fillChan, "buy")
			calcNetPosition()
		} else if best.bid.adjPrice-best.ask.adjPrice >= cfg.Sec.EntryArb {
			// If arb exists, trade pair
			amount := math.Min(best.bid.amount, best.ask.amount)
			log.Printf("*** Arb Opportunity: %.4f for %.2f on %s vs %s ***\n", best.bid.adjPrice-best.ask.adjPrice, amount, best.bid.exg, best.ask.exg)
			sendPair(best.bid, best.ask, amount)
			calcNetPosition()
		} else if (best.bid.exg.Position() >= cfg.Sec.MinOrder && best.ask.exg.Position() <= -cfg.Sec.MinOrder) &&
			(best.bid.adjPrice-best.ask.adjPrice >= cfg.Sec.ExitArb) {
			// If inter-exchange position can be exited, trade pair
			available := math.Min(best.bid.amount, best.ask.amount)
			needed := math.Min(best.bid.exg.Position(), -best.ask.exg.Position())
			amount := math.Min(available, needed)
			log.Printf("*** Exit Opportunity: %.4f for %.2f on %s vs %s ***\n", best.bid.adjPrice-best.ask.adjPrice, amount, best.bid.exg, best.ask.exg)
			sendPair(best.bid, best.ask, amount)
			calcNetPosition()
		}

		if cfg.Sec.PrintOn {
			printResults(best)
		}
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
func printResults(best bestMarket) {
	clearScreen()

	fmt.Println("   Positions:")
	fmt.Println("----------------")
	for _, exg := range exchanges {
		fmt.Printf("%-8s %7.2f\n", exg, exg.Position())
	}
	fmt.Println("----------------")
	fmt.Println("\nBest Market:")
	fmt.Printf("%v %.4f (%.4f) for %.2f / %.2f at %.4f (%.4f) %v\n",
		best.bid.exg, best.bid.orderPrice, best.bid.adjPrice, best.bid.amount, best.ask.amount, best.ask.orderPrice, best.ask.adjPrice, best.ask.exg)
	fmt.Println("\nOpportunity:")
	fmt.Printf("%.4f for %.2f\n", best.bid.adjPrice-best.ask.adjPrice, math.Min(best.bid.amount, best.ask.amount))
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
