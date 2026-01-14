package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	// Log startup with env info
	log.Printf("Starting full-app on :%s", port)
	log.Printf("Environment: %s", os.Getenv("ENVIRONMENT"))
	log.Printf("Log level: %s", os.Getenv("LOG_LEVEL"))

	// Check for secrets (don't log the actual values!)
	if os.Getenv("API_KEY") != "" {
		log.Printf("API_KEY: [set]")
	}
	if os.Getenv("DB_PASSWORD") != "" {
		log.Printf("DB_PASSWORD: [set]")
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)

		response := map[string]interface{}{
			"app":         "full-app",
			"time":        time.Now().Format(time.RFC3339),
			"environment": os.Getenv("ENVIRONMENT"),
			"version":     os.Getenv("VERSION"),
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	})

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"status":"healthy"}`)
	})

	http.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"status":"ready"}`)
	})

	http.HandleFunc("/env", func(w http.ResponseWriter, r *http.Request) {
		// Show env vars (redact secrets)
		env := map[string]string{
			"ENVIRONMENT": os.Getenv("ENVIRONMENT"),
			"LOG_LEVEL":   os.Getenv("LOG_LEVEL"),
			"VERSION":     os.Getenv("VERSION"),
			"API_KEY":     redact(os.Getenv("API_KEY")),
			"DB_PASSWORD": redact(os.Getenv("DB_PASSWORD")),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(env)
	})

	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func redact(s string) string {
	if s == "" {
		return "[not set]"
	}
	return "[redacted]"
}
