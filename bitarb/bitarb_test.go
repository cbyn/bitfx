package main

import (
	"math"
	"testing"
)

type neededArb struct {
	buyExgPos, sellExgPos, arb float64
}

var neededArbTests = []neededArb{
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

func TestCalculateNeededArb(t *testing.T) {
	cfg.Sec.MaxArb = 2
	cfg.Sec.MinArb = -1
	cfg.Sec.MaxPosition = 500

	for _, neededArb := range neededArbTests {
		arb := calcNeededArb(neededArb.buyExgPos, neededArb.sellExgPos)
		if math.Abs(arb-neededArb.arb) > .000001 {
			t.Errorf("For %.2f / %.2f expect %.2f, got %.2f\n", neededArb.buyExgPos, neededArb.sellExgPos, neededArb.arb, arb)
		}
	}
}
