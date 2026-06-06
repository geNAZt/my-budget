package main

import (
	"fmt"
	"github.com/wnjoon/go-yfinance/pkg/models"
	"github.com/wnjoon/go-yfinance/pkg/ticker"
)

func main() {
	params := models.HistoryParams{
		Interval: "1d",
		Period:   "max",
	}

	t, err := ticker.New("CSH2.PA")
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	defer t.Close()

	bars, err := t.History(params)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Printf("Fetched %d bars\n", len(bars))
	if len(bars) > 0 {
		for i := len(bars) - 1; i >= len(bars)-10; i-- {
			y, w := bars[i].Date.ISOWeek()
			fmt.Printf("Date: %s, Week %d/%d: %f\n", bars[i].Date.Format("2006-01-02"), w, y, bars[i].AdjClose)
		}
	}
}
