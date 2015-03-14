package main

import (
	"bitfx2/exchange"
	"bitfx2/okcoin"
	// "github.com/davecgh/go-spew/spew"
	"math"
	"testing"
)

func init() {
	cfg.Sec.MaxArb = 2
	cfg.Sec.MinArb = -1
	cfg.Sec.MaxPosition = 500
	cfg.Sec.MinOrder = 25
	cfg.Sec.MaxOrder = 50
}

type neededArb struct {
	buyExgPos, sellExgPos, arb float64
}

func TestCalculateNeededArb(t *testing.T) {
	neededArbTests := []neededArb{
		{500, -500, 2},
		{-500, 500, -1},
		{500, 500, .5},
		{-100, -100, .5},
		{0, 0, .5},
		{-250, 250, -.25},
		{250, -250, 1.25},
		{100, -100, .8},
		{0, -200, .8},
		{-200, 0, .2},
		{-100, 100, .2},
	}

	for _, neededArb := range neededArbTests {
		arb := calcNeededArb(neededArb.buyExgPos, neededArb.sellExgPos)
		if math.Abs(arb-neededArb.arb) > .000001 {
			t.Errorf("For %.2f / %.2f expect %.2f, got %.2f\n", neededArb.buyExgPos, neededArb.sellExgPos, neededArb.arb, arb)
		}
	}
}

func TestFilterBook(t *testing.T) {
	testBook := &exchange.Book{
		Exg: okcoin.New("", "", "", "", 1, 0.002),
		Bids: exchange.BidItems{
			0: {Price: 1.90, Amount: 10},
			1: {Price: 1.80, Amount: 10},
			2: {Price: 1.70, Amount: 100},
		},
		Asks: exchange.AskItems{
			0: {Price: 2.10, Amount: 10},
			1: {Price: 2.20, Amount: 20},
			2: {Price: 2.30, Amount: 10},
		},
	}

	market := filterBook(testBook)

	if math.Abs(market.bid.orderPrice-1.70) > .000001 {
		t.Errorf("Wrong bid order price")
	}
	if math.Abs(market.bid.amount-50) > .000001 {
		t.Errorf("Wrong bid amount")
	}
	adjPrice := ((1.90*10 + 1.80*10 + 1.70*30) / 50) * (1 - .002)
	if math.Abs(market.bid.adjPrice-adjPrice) > .000001 {
		t.Errorf("Wrong bid adjusted price")
	}
	if math.Abs(market.ask.orderPrice-2.20) > .000001 {
		t.Errorf("Wrong ask order price")
	}
	if math.Abs(market.ask.amount-30) > .000001 {
		t.Errorf("Wrong ask amount")
	}
	adjPrice = ((2.10*10 + 2.20*20) / 30) * (1 + .002)
	if math.Abs(market.ask.adjPrice-adjPrice) > .000001 {
		t.Errorf("Wrong ask adjusted price")
	}

	testBook = &exchange.Book{
		Exg: okcoin.New("", "", "", "", 2, 0.002),
		Bids: exchange.BidItems{
			0: {Price: 1.90, Amount: 30},
			1: {Price: 1.80, Amount: 10},
			2: {Price: 1.70, Amount: 100},
		},
		Asks: exchange.AskItems{
			0: {Price: 2.10, Amount: 100},
			1: {Price: 2.20, Amount: 20},
			2: {Price: 2.30, Amount: 10},
		},
	}

	market = filterBook(testBook)

	if math.Abs(market.bid.orderPrice-1.90) > .000001 {
		t.Errorf("Wrong bid order price")
	}
	if math.Abs(market.bid.amount-30) > .000001 {
		t.Errorf("Wrong bid amount")
	}
	adjPrice = 1.90 * (1 - .002)
	if math.Abs(market.bid.adjPrice-adjPrice) > .000001 {
		t.Errorf("Wrong bid adjusted price")
	}
	if math.Abs(market.ask.orderPrice-2.10) > .000001 {
		t.Errorf("Wrong ask order price")
	}
	if math.Abs(market.ask.amount-50) > .000001 {
		t.Errorf("Wrong ask amount")
	}
	adjPrice = 2.10 * (1 + .002)
	if math.Abs(market.ask.adjPrice-adjPrice) > .000001 {
		t.Errorf("Wrong ask adjusted price")
	}
}

func TestFindBestBid(t *testing.T) {
	markets := make(map[exchange.Exchange]filteredBook)
	exg1 := okcoin.New("", "", "", "", 1, 0.002)
	exg2 := okcoin.New("", "", "", "", 1, 0.002)
	exg3 := okcoin.New("", "", "", "", 1, 0.002)
	markets[exg1] = filteredBook{bid: market{adjPrice: 2.00}}
	markets[exg2] = filteredBook{bid: market{adjPrice: 1.99}}
	markets[exg3] = filteredBook{bid: market{adjPrice: 1.98}}
	if findBestBid(markets) != exg1 {
		t.Error("Returned wrong best bid")
	}
	exg1.SetPosition(-490)
	if findBestBid(markets) != exg2 {
		t.Error("Returned wrong best bid after position update")
	}
}

func TestFindBestAsk(t *testing.T) {
	markets := make(map[exchange.Exchange]filteredBook)
	exg1 := okcoin.New("", "", "", "", 1, 0.002)
	exg2 := okcoin.New("", "", "", "", 1, 0.002)
	exg3 := okcoin.New("", "", "", "", 1, 0.002)
	markets[exg1] = filteredBook{ask: market{adjPrice: 1.98}}
	markets[exg2] = filteredBook{ask: market{adjPrice: 1.99}}
	markets[exg3] = filteredBook{ask: market{adjPrice: 2.00}}
	if findBestAsk(markets) != exg1 {
		t.Error("Returned wrong best ask")
	}
	exg1.SetPosition(490)
	if findBestAsk(markets) != exg2 {
		t.Error("Returned wrong best ask after position update")
	}
}

//
//
//
//
//
//
//
//
//
//
//
//
//
//
//
//
//
