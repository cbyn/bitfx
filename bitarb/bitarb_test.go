package main

import (
	"bitfx2/bitfinex"
	"bitfx2/exchange"
	"bitfx2/okcoin"
	// "github.com/davecgh/go-spew/spew"
	"math"
	"testing"
)

func init() {
	cfg.Sec.MaxArb = .02
	cfg.Sec.MinArb = -.01
	cfg.Sec.FXPremium = .01
	cfg.Sec.MinOrder = 25
	cfg.Sec.MaxOrder = 50
}

type neededArb struct {
	buyExgPos, sellExgPos, arb float64
}

func TestCalculateNeededArb(t *testing.T) {
	// Test without FX
	neededArbTests := []neededArb{
		{500, -500, .02},
		{-500, 500, -.01},
		{500, 500, .005},
		{-100, -100, .005},
		{0, 0, .005},
		{-250, 250, -.0025},
		{250, -250, .0125},
		{100, -100, .008},
		{0, -200, .008},
		{-200, 0, .002},
		{-100, 100, .002},
	}
	buyExg := okcoin.New("", "", "", "usd", 1, 0.002, 500)
	sellExg := bitfinex.New("", "", "", "usd", 2, 0.001, 500)

	for _, neededArb := range neededArbTests {
		buyExg.SetPosition(neededArb.buyExgPos)
		sellExg.SetPosition(neededArb.sellExgPos)
		arb := calcNeededArb(buyExg, sellExg)
		if math.Abs(arb-neededArb.arb) > .000001 {
			t.Errorf("For %.4f / %.4f expect %.4f, got %.4f\n", buyExg.Position(), sellExg.Position(), neededArb.arb, arb)
		}
	}

	// Test with FX
	neededArbTests = []neededArb{
		{500, -500, .03},
		{-500, 500, -.01},
		{500, 500, .01},
		{-100, -100, .01},
		{0, 0, .01},
		{-250, 250, 0},
		{250, -250, .02},
		{100, -100, .014},
		{0, -200, .014},
		{-200, 0, .006},
		{-100, 100, .006},
	}
	buyExg = okcoin.New("", "", "", "cny", 1, 0.002, 500)
	sellExg = bitfinex.New("", "", "", "usd", 2, 0.001, 500)

	for _, neededArb := range neededArbTests {
		buyExg.SetPosition(neededArb.buyExgPos)
		sellExg.SetPosition(neededArb.sellExgPos)
		arb := calcNeededArb(buyExg, sellExg)
		if math.Abs(arb-neededArb.arb) > .000001 {
			t.Errorf("For %.4f / %.4f expect %.4f, got %.4f\n", buyExg.Position(), sellExg.Position(), neededArb.arb, arb)
		}
	}

}

func TestFilterBook(t *testing.T) {
	testBook := exchange.Book{
		Exg: okcoin.New("", "", "", "usd", 1, 0.002, 500),
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
	market := filterBook(testBook, 1)
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
	// Same test but with FX adjustment
	fxPrice := 2.0
	market = filterBook(testBook, fxPrice)
	if math.Abs(market.bid.orderPrice-1.70) > .000001 {
		t.Errorf("Wrong bid order price")
	}
	if math.Abs(market.bid.amount-50) > .000001 {
		t.Errorf("Wrong bid amount")
	}
	adjPrice = ((1.90*10 + 1.80*10 + 1.70*30) / 50) * (1 - .002) / fxPrice
	if math.Abs(market.bid.adjPrice-adjPrice) > .000001 {
		t.Errorf("Wrong bid adjusted price")
	}
	if math.Abs(market.ask.orderPrice-2.20) > .000001 {
		t.Errorf("Wrong ask order price")
	}
	if math.Abs(market.ask.amount-30) > .000001 {
		t.Errorf("Wrong ask amount")
	}
	adjPrice = ((2.10*10 + 2.20*20) / 30) * (1 + .002) / fxPrice
	if math.Abs(market.ask.adjPrice-adjPrice) > .000001 {
		t.Errorf("Wrong ask adjusted price")
	}

	testBook = exchange.Book{
		Exg: okcoin.New("", "", "", "usd", 2, 0.002, 500),
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
	market = filterBook(testBook, 1)
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
	// Same test as above, but wiht FX adjustment
	fxPrice = 3.0
	market = filterBook(testBook, fxPrice)
	if math.Abs(market.bid.orderPrice-1.90) > .000001 {
		t.Errorf("Wrong bid order price")
	}
	if math.Abs(market.bid.amount-30) > .000001 {
		t.Errorf("Wrong bid amount")
	}
	adjPrice = 1.90 * (1 - .002) / fxPrice
	if math.Abs(market.bid.adjPrice-adjPrice) > .000001 {
		t.Errorf("Wrong bid adjusted price")
	}
	if math.Abs(market.ask.orderPrice-2.10) > .000001 {
		t.Errorf("Wrong ask order price")
	}
	if math.Abs(market.ask.amount-50) > .000001 {
		t.Errorf("Wrong ask amount")
	}
	adjPrice = 2.10 * (1 + .002) / fxPrice
	if math.Abs(market.ask.adjPrice-adjPrice) > .000001 {
		t.Errorf("Wrong ask adjusted price")
	}
}

func TestFindBestBid(t *testing.T) {
	markets := make(map[exchange.Exchange]filteredBook)
	exg1 := okcoin.New("", "", "", "usd", 1, 0.002, 500)
	exg2 := okcoin.New("", "", "", "usd", 1, 0.002, 500)
	exg3 := okcoin.New("", "", "", "usd", 1, 0.002, 500)
	markets[exg1] = filteredBook{bid: market{adjPrice: 2.00, amount: 500}}
	markets[exg2] = filteredBook{bid: market{adjPrice: 1.99}}
	markets[exg3] = filteredBook{bid: market{adjPrice: 1.98}}
	if math.Abs(findBestBid(markets).adjPrice-2.00) > .000001 {
		t.Error("Returned wrong best bid")
	}
	exg1.SetPosition(-490)
	if math.Abs(findBestBid(markets).adjPrice-1.99) > .000001 {
		t.Error("Returned wrong best bid after position update")
	}
	exg1.SetPosition(-250)
	if math.Abs(findBestBid(markets).amount-250) > .000001 {
		t.Error("Returned wrong best bid amount after position update")
	}
}

func TestFindBestAsk(t *testing.T) {
	markets := make(map[exchange.Exchange]filteredBook)
	exg1 := okcoin.New("", "", "", "usd", 1, 0.002, 500)
	exg2 := okcoin.New("", "", "", "usd", 1, 0.002, 500)
	exg3 := okcoin.New("", "", "", "usd", 1, 0.002, 500)
	markets[exg1] = filteredBook{ask: market{adjPrice: 1.98, amount: 500}}
	markets[exg2] = filteredBook{ask: market{adjPrice: 1.99}}
	markets[exg3] = filteredBook{ask: market{adjPrice: 2.00}}
	if math.Abs(findBestAsk(markets).adjPrice-1.98) > .000001 {
		t.Error("Returned wrong best ask")
	}
	exg1.SetPosition(490)
	if math.Abs(findBestAsk(markets).adjPrice-1.99) > .000001 {
		t.Error("Returned wrong best ask after position update")
	}
	exg1.SetPosition(250)
	if math.Abs(findBestAsk(markets).amount-250) > .000001 {
		t.Error("Returned wrong best ask after position update")
	}
}

func TestFindBestArb(t *testing.T) {
	// No opportunity
	markets := make(map[exchange.Exchange]filteredBook)
	exg1 := okcoin.New("", "", "", "usd", 1, 0.002, 500)
	exg2 := okcoin.New("", "", "", "usd", 1, 0.002, 500)
	exg3 := okcoin.New("", "", "", "usd", 1, 0.002, 500)
	markets[exg1] = filteredBook{
		bid: market{adjPrice: 1.98, amount: 50, exg: exg1},
		ask: market{adjPrice: 2.00, amount: 50, exg: exg1},
	}
	markets[exg2] = filteredBook{
		bid: market{adjPrice: 1.99, amount: 50, exg: exg2},
		ask: market{adjPrice: 2.01, amount: 50, exg: exg2},
	}
	markets[exg3] = filteredBook{
		bid: market{adjPrice: 2.00, amount: 50, exg: exg3},
		ask: market{adjPrice: 2.02, amount: 50, exg: exg3},
	}
	if _, _, exists := findBestArb(markets); exists {
		t.Errorf("Should be no arb opportunity")
	}
	// Change positions to create an exit opportunity
	exg1.SetPosition(-500)
	exg3.SetPosition(500)
	bestBid, bestAsk, exists := findBestArb(markets)
	if !exists || bestBid.exg != exg3 || bestAsk.exg != exg1 {
		t.Errorf("Should be an exit opportunity after position update")
	}
	exg1.SetPosition(0)
	exg3.SetPosition(0)

	// Create an arb opportunity
	markets[exg1] = filteredBook{
		bid: market{adjPrice: 2.03, amount: 50, exg: exg1},
		ask: market{adjPrice: 2.04, amount: 50, exg: exg1},
	}
	markets[exg2] = filteredBook{
		bid: market{adjPrice: 2.04, amount: 50, exg: exg2},
		ask: market{adjPrice: 2.05, amount: 50, exg: exg2},
	}
	markets[exg3] = filteredBook{
		bid: market{adjPrice: 1.99, amount: 50, exg: exg3},
		ask: market{adjPrice: 2.00, amount: 50, exg: exg3},
	}
	bestBid, bestAsk, exists = findBestArb(markets)
	if !exists || bestBid.exg != exg2 || bestAsk.exg != exg3 {
		t.Errorf("Should be an arb opportunity")
	}

	// Set exg3 postion to only allow for 30 more
	exg3.SetPosition(470)
	_, bestAsk, _ = findBestArb(markets)
	if math.Abs(bestAsk.amount-30) > .000001 {
		t.Errorf("Should be a decrease in best ask amount")
	}

	// Change exg3 postion
	exg2.SetPosition(-500)
	bestBid, _, _ = findBestArb(markets)
	if bestBid.exg != exg1 {
		t.Errorf("Best bid exchange should have changed")
	}
}
