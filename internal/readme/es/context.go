// Package es compiles the Go examples of README.md, which internal/cmd/readme
// writes here.
package es

import (
	"context"

	"github.com/flashpi1/flashpi-go"
)

// What the examples take from the ones before them, or from your own code.
var (
	ctx          = context.Background()
	client       *flashpi.Client
	invoice      = &flashpi.Document{}
	body         flashpi.CreateInvoice
	documentID   string
	issuerID     string
	order        struct{ ID string }
	saveForRetry = func(flashpi.CreateInvoice, string) {}
)
