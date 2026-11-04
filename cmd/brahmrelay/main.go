// Command brahmrelay is the BRAHM Drop "connect from anywhere" relay server.
// Run it on a VPS; devices register with a code and can then be reached from
// any network by that code.
//
// Brahm InnerBrain Technologies Pvt. Ltd. — Developed by Sandeep Kushwah.
package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"

	"brahmdrop/internal/relay"
)

func main() {
	var (
		port     = flag.Int("port", relay.DefaultPort, "relay TCP port")
		httpPort = flag.Int("http", 0, "optional HTTP status port (0 = off)")
		quiet    = flag.Bool("quiet", false, "reduce logging")
	)
	flag.Parse()

	srv := relay.NewServer(!*quiet)

	if *httpPort != 0 {
		go func() {
			http.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprintf(w, "BRAHM Drop relay — %s\n", srv.Stats())
			})
			_ = http.ListenAndServe(fmt.Sprintf(":%d", *httpPort), nil)
		}()
	}

	addr := fmt.Sprintf(":%d", *port)
	fmt.Println("========================================================")
	fmt.Println("  BRAHM Drop Relay")
	fmt.Println("  Brahm InnerBrain Technologies Pvt. Ltd.")
	fmt.Println("  Developed by Sandeep Kushwah")
	fmt.Println("========================================================")
	fmt.Printf("  Listening on TCP %s\n", addr)
	fmt.Println("  Point devices here with:  --relay <this-server-ip>:" + fmt.Sprint(*port))
	fmt.Println("========================================================")

	if err := srv.ListenAndServe(addr); err != nil {
		fmt.Fprintln(os.Stderr, "relay error:", err)
		os.Exit(1)
	}
}
