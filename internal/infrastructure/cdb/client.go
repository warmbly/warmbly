package cdb

import (
	"fmt"
	"log"
	"os"
	"time"

	gocqlastra "github.com/datastax/gocql-astra"
	"github.com/gocql/gocql"
	"github.com/warmbly/warmbly/internal/config"
)

type Client struct {
	*gocql.Session
}

func NewClient(
	cfg *config.AstraConfig,
) (*Client, error) {
	// Skip Astra connection in dev - no real Astra instance available
	if os.Getenv("APP_ENV") == "dev" {
		log.Println("Warning: Skipping Astra/Cassandra connection in dev mode")
		return nil, nil
	}

	if len(os.Getenv("ASTRA_APPLICATION_TOKEN")) > 0 {
		if len(os.Getenv("ASTRA_DB_ID")) == 0 {
			return nil, fmt.Errorf("ASTRA_DB_ID is required when ASTRA_APPLICATION_TOKEN is set")
		}
	}

	cluster, err := gocqlastra.NewClusterFromURL("https://api.astra.datastax.com", os.Getenv("ASTRA_DB_ID"), os.Getenv("ASTRA_APPLICATION_TOKEN"), 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("unable to load cluster from astra: %w", err)
	}

	cluster.Timeout = 30 * time.Second
	session, err := gocql.NewSession(*cluster)
	if err != nil {
		return nil, fmt.Errorf("unable to connect astra session: %w", err)
	}

	return &Client{Session: session}, nil
}
