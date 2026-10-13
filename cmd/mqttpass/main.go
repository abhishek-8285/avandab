// Command mqttpass prints one mosquitto password-file line for a username,
// using the exact hash the app provisions for drivers
// (internal/mqttservice.NewBrokerSecret — docs/13 §5.2).
//
// Operators need it for credentials the app does not own: the backend
// superuser, and hardware devices authenticated as their IMEI. Driver
// credentials come from the DB instead — see scripts/mqtt-export-credentials.sh.
//
// Usage:
//
//	go run ./cmd/mqttpass avandab_backend
//	<secret>
//	avandab_backend:$7$1000$...
//
// The secret is printed once and never stored: append the line to the broker's
// password_file and keep the secret in the service's environment.
package main

import (
	"fmt"
	"os"

	"transport-app/internal/mqttservice"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: mqttpass <username>")
		os.Exit(2)
	}
	username := os.Args[1]
	if err := mqttservice.ValidateBrokerUsername(username); err != nil {
		fmt.Fprintf(os.Stderr, "invalid username: %v\n", err)
		os.Exit(1)
	}
	secret, hash, err := mqttservice.NewBrokerSecret()
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate credential: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(secret)
	fmt.Println(mqttservice.BrokerPasswordFileLine(username, hash))
}
