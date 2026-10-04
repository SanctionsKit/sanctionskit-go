package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/SanctionsKit/sanctionskit-go"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	client, err := sanctionskit.New(os.Getenv("SANCTIONSKIT_API_KEY"), nil)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	result, err := client.CreateScreening(ctx, sanctionskit.ScreeningRequest{
		Subject: sanctionskit.Subject{Name: "Alex Morgan", EntityType: "person", BirthDate: "1984"},
		Package: "sandbox@1", Retention: "standard", Reference: "example-customer-001",
	}, os.Getenv("REQUEST_KEY"))
	if err != nil {
		return err
	}
	evidence, err := client.GetEvidence(ctx, result.Data.ID)
	if err != nil {
		return err
	}
	fmt.Printf("Screening %s: %s. Evidence format: %s. Review still required.\n", result.Data.ID, result.Data.Status, evidence.Evidence.Format)
	return nil
}
