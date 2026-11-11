//go:build ignore
package main
import ("fmt";"time"
 "github.com/andrelbuzzo/itm/internal/ingest"
 "github.com/andrelbuzzo/itm/internal/windows")
func main(){
 lr,_ := ingest.Logs("fixtures/pool-exhaustion/logs.jsonl")
 // replicate the binning the detector does
 first := lr.Records[0].TS
 fmt.Println("bin=10s, per-bin total/err/rate:")
 for i := 0; i < 12; i++ {
  start := first.Add(time.Duration(i)*10*1e9)
  end := start.Add(10*1e9)
  tot, errs := 0, 0
  for _, l := range lr.Records {
   if !l.TS.Before(start) && l.TS.Before(end) {
    tot++
    if l.Level.IsError() { errs++ }
   }
  }
  rate := 0.0
  if tot > 0 { rate = float64(errs)/float64(tot) }
  fmt.Printf("  bin%02d %v tot=%2d err=%2d rate=%.2f\n", i, start.Format("15:04:05"), tot, errs, rate)
 }
 _ = windows.DefaultConfig
}
