package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/rs/cors"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/raveesh/ai-akinator/gen/akinator/v1/akinatorv1connect"
	"github.com/raveesh/ai-akinator/internal/data"
	"github.com/raveesh/ai-akinator/internal/server"
)

func main() {
	addr := flag.String("addr", envOr("ADDR", ":8080"), "listen address")
	flag.Parse()

	players, err := data.Load()
	if err != nil {
		log.Fatalf("load players: %v", err)
	}
	log.Printf("loaded %d players", len(players))

	svc := server.New(players)
	mux := http.NewServeMux()
	path, handler := akinatorv1connect.NewAkinatorServiceHandler(svc)
	mux.Handle(path, handler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// Allow the SvelteKit dev server (and any local origin) to call us.
	corsHandler := cors.New(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{http.MethodGet, http.MethodPost, http.MethodOptions},
		AllowedHeaders: []string{"*"},
		ExposedHeaders: []string{"Grpc-Status", "Grpc-Message", "Grpc-Status-Details-Bin"},
	}).Handler(mux)

	srv := &http.Server{
		Addr:              *addr,
		Handler:           h2c.NewHandler(corsHandler, &http2.Server{}),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("listening on %s", *addr)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
