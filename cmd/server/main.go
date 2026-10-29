package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"myanime/internal/app"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8767", "HTTP listen address (use a TLS reverse proxy for remote access)")
	data := flag.String("data-dir", os.Getenv("MYANIME_DATA_DIR"), "SQLite data directory")
	frontend := flag.String("frontend-dir", os.Getenv("MYANIME_FRONTEND_DIR"), "built frontend directory")
	flag.Parse()
	host, _, err := net.SplitHostPort(*listen)
	if err != nil {
		log.Fatal(err)
	}
	ip := net.ParseIP(host)
	if (ip == nil || !ip.IsLoopback()) && os.Getenv("MYANIME_BEHIND_TLS_PROXY") != "1" {
		log.Fatal("Non-loopback listen requires MYANIME_BEHIND_TLS_PROXY=1 and an authenticated TLS reverse proxy")
	}
	var proxies []string
	if v := os.Getenv("MYANIME_TRUSTED_PROXIES"); v != "" {
		proxies = strings.Split(v, ",")
	}
	frontendFS, err := frontendAssets()
	if err != nil {
		log.Fatal(err)
	}
	application, err := app.New(app.Config{DataDir: *data, FrontendDir: *frontend, FrontendFS: frontendFS, Mode: "web", PublicOrigin: os.Getenv("MYANIME_PUBLIC_ORIGIN"), TrustedProxyCIDRs: proxies, Development: os.Getenv("MYANIME_DEV") == "1", DevOrigin: os.Getenv("MYANIME_DEV_ORIGIN")})
	if err != nil {
		log.Fatal(err)
	}
	defer application.Close()
	srv := &http.Server{Addr: *listen, Handler: application.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe() }()
	log.Printf("MyAnime listening at http://%s", *listen)
	select {
	case err = <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Print(err)
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err = srv.Shutdown(shutdown); err != nil {
			_ = srv.Close()
		}
	}
}
