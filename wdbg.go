//go:build ignore
package main
import ("fmt"
 "github.com/andrelbuzzo/itm/internal/ingest"
 "github.com/andrelbuzzo/itm/internal/windows")
func main(){
 lr, err := ingest.Logs("fixtures/pool-exhaustion/logs.jsonl")
 if err != nil { panic(err) }
 fmt.Printf("logs: %d accepted, first=%v last=%v\n", len(lr.Records),
  lr.FirstSeen.Format("15:04:05"), lr.LastSeen.Format("15:04:05"))
 cfg := windows.DefaultConfig()
 win := windows.Detect(lr.Records, cfg)
 fmt.Printf("detected=%v scanned=%d\n", win.Detected, win.Scanned)
 fmt.Printf("  base   %v .. %v\n", win.BaselineStart.Format("15:04:05"), win.BaselineEnd.Format("15:04:05"))
 fmt.Printf("  inc    %v .. %v\n", win.IncidentStart.Format("15:04:05"), win.IncidentEnd.Format("15:04:05"))
}
