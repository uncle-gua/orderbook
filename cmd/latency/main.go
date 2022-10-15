package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"fortio.org/fortio/stats"
	"github.com/geseq/orderbook"
	decimal "github.com/geseq/udecimal"
	"github.com/loov/hrtime"
)

type EmptyNotification struct {
}

func (e *EmptyNotification) PutOrder(m orderbook.MsgType, s orderbook.OrderStatus, orderID uint64, qty decimal.Decimal, err error) {
}

func (e *EmptyNotification) PutTrade(mOID, tOID uint64, mStatus, tStatus orderbook.OrderStatus, qty, price decimal.Decimal) {
}

func getPrice(bid, ask, diff decimal.Decimal, dec bool) (decimal.Decimal, decimal.Decimal) {
	if dec {
		bid = bid.Sub(diff)
		ask = ask.Sub(diff)
		return bid, ask
	}

	bid = bid.Add(diff)
	ask = ask.Add(diff)
	return bid, ask
}

func main() {
	runtime.LockOSThread()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		runtime.LockOSThread()
		log.Printf("Running on thread %d", syscall.Gettid())
		seed := flag.Int64("seed", time.Now().UnixNano(), "rand seed")
		duration := flag.Int("duration", 0, "benchmark duration in seconds")
		lb := flag.String("l", "50.0", "lower bound")
		ub := flag.String("u", "100.0", "upper bound")
		ms := flag.String("m", "0.25", "min spread")
		pd := flag.Uint64("p", 10, "print duration in seconds")
		gc := flag.Bool("gc", true, "use gc")
		flag.Parse()

		if !*gc {
			debug.SetGCPercent(-1)
		}
		rand := rand.New(rand.NewSource(*seed))

		lowerBound := decimal.MustParse(*lb)
		upperBound := decimal.MustParse(*ub)
		minSpread := decimal.MustParse(*ms)

		bid := lowerBound.Add(upperBound).Div(decimal.NewI(2, 0))
		ask := bid.Sub(minSpread)
		bidQty := decimal.NewI(10, 0)
		askQty := decimal.NewI(10, 0)

		on := &EmptyNotification{}
		ob := orderbook.NewOrderBook(on)
		ah := stats.NewHistogram(10, 1)
		ch := stats.NewHistogram(10, 1)

		var tok, buyID, sellID uint64
		var ops uint64

		// calibrate
		c := hrtime.TSC()
		c.ApproxDuration()
		c = hrtime.TSC()
		log.Println(hrtime.TSCSince(c).ApproxDuration())

		log.Println("starting latency benchmark")
		start := time.Now()
		end := time.Now().Add(time.Duration(*duration) * time.Second)
		var diff uint64
		for time.Now().Before(end) {
			var r = rand.Intn(10)
			dec := r < 5

			bid, ask = getPrice(bid, ask, minSpread, dec)
			if bid.LessThan(lowerBound) {
				bid, ask = getPrice(bid, ask, minSpread, false)
			} else if bid.GreaterThan(upperBound) {
				bid, ask = getPrice(bid, ask, minSpread, true)
			}

			ds := hrtime.TSC()
			tok = tok + 1
			s := hrtime.TSC()
			ob.CancelOrder(tok, buyID)
			ch.Record(float64(hrtime.TSCSince(s).ApproxDuration()))
			tok = tok + 1
			s = hrtime.TSC()
			ob.CancelOrder(tok, sellID)
			ch.Record(float64(hrtime.TSCSince(s).ApproxDuration()))
			tok = tok + 1
			buyID = tok
			tok = tok + 1
			sellID = tok
			s = hrtime.TSC()
			ob.AddOrder(buyID, buyID, orderbook.Limit, orderbook.Buy, bidQty, bid, decimal.Zero, orderbook.None)
			ah.Record(float64(hrtime.TSCSince(s).ApproxDuration()))
			s = hrtime.TSC()
			ob.AddOrder(sellID, sellID, orderbook.Limit, orderbook.Sell, askQty, ask, decimal.Zero, orderbook.None)
			ah.Record(float64(hrtime.TSCSince(s).ApproxDuration()))
			diff += uint64(hrtime.TSCSince(ds).ApproxDuration())
			atomic.AddUint64(&ops, 4) // 4 cancels and adds

			if uint64(time.Now().Sub(start).Seconds()) > *pd {
				fmt.Printf("ops/s: %d\n", ops/uint64(diff/1000_000_000))
				ops = 0
				start = time.Now()
				diff = 0
			}
		}

		ah.Print(os.Stdout, "Add Results", []float64{50, 75, 90, 95, 99, 99.9, 99.99, 100})
		ch.Print(os.Stdout, "Cancel Results", []float64{50, 75, 90, 95, 99, 99.9, 99.99, 100})

		wg.Done()
	}()

	wg.Wait()
}