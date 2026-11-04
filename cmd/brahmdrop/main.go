// Command brahmdrop runs the BRAHM Drop file-transfer app.
//
// BRAHM Drop — fast, private, offline file transfer across devices on the same
// Wi-Fi. No login, no ads, no cloud.
//
// Brahm InnerBrain Technologies Pvt. Ltd. — Developed by Sandeep Kushwah.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"brahmdrop/internal/config"
	"brahmdrop/internal/netutil"
	"brahmdrop/internal/server"
	"brahmdrop/internal/transfer"

	qrcode "github.com/skip2/go-qrcode"
)

func main() {
	var (
		name       = flag.String("name", "", "device name shown to other devices")
		port       = flag.Int("port", 0, "HTTP/transfer port (default 53330)")
		dir        = flag.String("dir", "", "folder where received files are saved")
		noBrowse   = flag.Bool("no-browser", false, "do not open the web UI in a browser")
		remoteOn   = flag.Bool("remote", false, "start remote-desktop hosting immediately")
		autoAccept = flag.Bool("auto-accept", false, "accept incoming files automatically (no prompt)")
		relayAddr  = flag.String("relay", "", "VPS relay address host:port for internet access")
		showVer    = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *showVer {
		fmt.Printf("%s v%s\n", config.AppName, config.Version)
		return
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		os.Exit(1)
	}
	if *name != "" {
		cfg.DeviceName = *name
	}
	if *port != 0 {
		cfg.HTTPPort = *port
	}
	if *dir != "" {
		cfg.SaveDir = *dir
		_ = os.MkdirAll(cfg.SaveDir, 0o755)
	}
	if *autoAccept {
		cfg.AutoAccept = true
	}
	_ = cfg.Save()

	hist := transfer.NewStore(transfer.DefaultHistoryPath(config.Dir()))
	srv := server.New(cfg, hist)
	if err := srv.StartDiscovery(); err != nil {
		fmt.Fprintln(os.Stderr, "discovery warning (transfer still works):", err)
	}

	httpSrv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler: srv.Handler(),
	}

	url := fmt.Sprintf("http://%s:%d", netutil.PrimaryIP(), cfg.HTTPPort)
	printBanner(cfg, url)

	if *remoteOn {
		if code, err := srv.EnableRemote(); err == nil {
			fmt.Printf("  Remote desktop: ON — pairing code %s\n", code)
		} else {
			fmt.Println("  Remote desktop could not start:", err)
		}
	}

	// Internet relay: command-line flag wins, else whatever was saved in config.
	effectiveRelay := *relayAddr
	if effectiveRelay == "" {
		effectiveRelay = cfg.RelayAddr
	} else if effectiveRelay != cfg.RelayAddr {
		cfg.RelayAddr = effectiveRelay
		_ = cfg.Save()
	}
	if effectiveRelay != "" {
		code := srv.EnableRelay(effectiveRelay)
		fmt.Printf("  Internet relay: %s — your connection code is %s\n", effectiveRelay, code)
	}

	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintln(os.Stderr, "server error:", err)
			os.Exit(1)
		}
	}()

	if !*noBrowse {
		go openBrowser(fmt.Sprintf("http://127.0.0.1:%d", cfg.HTTPPort))
	}

	// graceful shutdown
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	fmt.Println("\nShutting down…")
	srv.Discovery().Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(ctx)
}

func printBanner(cfg *config.Config, url string) {
	line := "========================================================"
	fmt.Println(line)
	fmt.Printf("  %s  v%s\n", config.AppName, config.Version)
	fmt.Printf("  %s\n", config.Company)
	fmt.Printf("  Developed by %s\n", config.Developer)
	fmt.Println(line)
	fmt.Printf("  Device name : %s\n", cfg.DeviceName)
	fmt.Printf("  Saving to   : %s\n", cfg.SaveDir)
	fmt.Println()
	fmt.Println("  Open on THIS device:")
	fmt.Printf("     http://127.0.0.1:%d\n", cfg.HTTPPort)
	fmt.Println()
	fmt.Println("  Open from OTHER devices on the same Wi-Fi:")
	for _, ip := range netutil.LocalIPs() {
		fmt.Printf("     http://%s:%d\n", ip, cfg.HTTPPort)
	}
	fmt.Println()
	fmt.Println("  Scan to connect a phone (no install needed):")
	if q, err := qrcode.New(url, qrcode.Low); err == nil {
		fmt.Print(q.ToSmallString(false))
	}
	fmt.Println(line)
	fmt.Println("  Press Ctrl+C to stop.")
	fmt.Println(line)
}

func openBrowser(url string) {
	time.Sleep(600 * time.Millisecond)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default: // linux and others
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
