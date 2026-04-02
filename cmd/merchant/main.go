package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	paymentsgate "example.com/kgs-payment"
	"example.com/kgs-payment/internal/app"
)

func main() {
	envPath := flag.String("env", ".env", "path to env file")
	flag.Parse()

	environment, err := paymentsgate.LoadEnvironment(*envPath)
	if err != nil {
		log.Fatal(err)
	}

	application, err := app.NewApplication(app.Config{
		Environment: environment,
	})
	if err != nil {
		log.Fatal(err)
	}

	addr := getenv("PAYMENTSGATE_ADDR", ":8080")
	log.Printf("paymentsgate merchant demo listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, application.Routes()))
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
