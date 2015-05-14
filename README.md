Trading system bitarb/bitarb.go conducts arbitrage across Bitfinex, OKCoin USD, OKCoin CNY, and BTC China. Position management is fully automated. The system is functional and can be run autonomously but is not intended as a turn-key system for general use. 

Configuration settings are in bitarb/bitarb.gcfg. Environment variables <exchange>_KEY and <exchange>_SECRET are needed for access to each exchange. New exchanges can be added by implementing exchange.Interface.
