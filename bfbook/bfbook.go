package main

import (
	"bitfx2/bitfinex"
	"bitfx2/exchange"
	"fmt"
	"log"
	"os"
	"os/exec"
)

var bf = bitfinex.New(os.Getenv("BITFINEX_KEY"), os.Getenv("BITFINEX_SECRET"), "ltc", "usd", 2, 0.001, 1)

func main() {
	filename := "bfbook.log"
	logFile, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatal(err)
	}
	log.SetOutput(logFile)
	log.Println("Starting new run")

	doneChan := make(chan bool, 1)
	bookChan := make(chan exchange.Book)
	if book := bf.CommunicateBook(bookChan, doneChan); book.Error != nil {
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
			fmt.Printf("%-10s%-10.4f%8.2f\n", "", item.Price, item.Amount)
		}
		for _, item := range book.Bids {
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
