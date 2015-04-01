// Tester program for displaying OKCoin book data to terminal

package main

import (
	"bitfx/exchange"
	"bitfx/forex"
	"bitfx/okcoin"
	"fmt"
	"log"
	"os"
	"os/exec"
)

var (
	ok  = okcoin.New("", "", "ltc", "cny", 0, 0, 0, 0)
	cny float64
)

func main() {
	filename := "okbook.log"
	logFile, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatal(err)
	}
	log.SetOutput(logFile)
	log.Println("Starting new run")
	fxChan := make(chan forex.Quote)
	fxDoneChan := make(chan bool)
	quote := forex.CommunicateFX("cny", fxChan, fxDoneChan)
	if quote.Error != nil || quote.Price == 0 {
		log.Fatal(quote.Error)
	}
	cny = quote.Price

	doneChan := make(chan bool, 1)
	bookChan := make(chan exchange.Book)
	if book := ok.CommunicateBook(bookChan, doneChan); book.Error != nil {
		log.Fatal(book.Error)
	}
	inputChan := make(chan rune)
	go checkStdin(inputChan)

Loop:
	for {
		select {
		case book := <-bookChan:
			printBook(book)
		case <-inputChan:
			doneChan <- true
			fxDoneChan <- true
			break Loop
		}
	}

}

// Check for any user input
func checkStdin(inputChan chan<- rune) {
	var ch rune
	fmt.Scanf("%c", &ch)
	inputChan <- ch
}

// Print book data from each exchange
func printBook(book exchange.Book) {
	clearScreen()
	if book.Error != nil {
		log.Println(book.Error)
	} else {
		fmt.Println("----------------------------")
		fmt.Printf("%-10s%-10s%8s\n", " Bid", "  Ask", "Size ")
		fmt.Println("----------------------------")
		for i := range book.Asks {
			item := book.Asks[len(book.Asks)-1-i]
			fmt.Printf("%-10s%-10.4f%8.2f\n", "", item.Price/cny, item.Amount)
		}
		for _, item := range book.Bids {
			fmt.Printf("%-10.4f%-10.2s%8.2f\n", item.Price/cny, "", item.Amount)
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
