Trading system bitarb/bitarb.go executes cryptocurrency arbitrage trades accross Bitfinex, OKCoin USD, OKCoin CNY, and BTC China. The amount of edge required for an arbitrage trade is determined by the open positions on each exchange. The system is functional and can be run autonomously but is not intended as a turn-key system for general use. 

Configuration settings are in bitarb/bitarb.gcfg. Environment variables <exchange>_KEY and <exchange>_SECRET are needed for exchange access.
