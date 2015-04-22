// Tester program for displaying Bitfinex book data to terminal

package main

import (
	"bitfx/bitfinex"
	"bitfx/exchange"
	"fmt"
	"log"
	"os"
	"os/exec"
)

var bf = bitfinex.New("", "", "ltc", "usd", 0, 0, 0, 0)

func main() {
	filename := "bfbook.log"
	logFile, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatal(err)
	}
	log.SetOutput(logFile)
	log.Println("Starting new run")

	bookChan := make(chan exchange.Book)
	if book := bf.CommunicateBook(bookChan); book.Error != nil {
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
			bf.Done()
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
