// Command virtual-plc runs the virtual controller core and serves the
// React wiring/observation UI. It never connects to real industrial devices.
package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"virtual-plc/internal/api"
	"virtual-plc/internal/plc"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	tick := flag.Duration("tick", 100*time.Millisecond, "scan period")
	flag.Parse()

	c := plc.NewController(*tick)
	// First round ships with the demonstration program as version 1.
	c.Load(plc.DemoProgram(1))

	h := api.New(c)
	log.Printf("virtual controller listening on http://localhost%s (scan %s, no physical I/O)", *addr, *tick)
	if err := http.ListenAndServe(*addr, h); err != nil {
		log.Fatal(err)
	}
}
