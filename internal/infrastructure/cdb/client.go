package cdb

import (
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
	var err error
	var cluster *gocql.ClusterConfig

	if len(os.Getenv("ASTRA_APPLICATION_TOKEN")) > 0 {
		if len(os.Getenv("ASTRA_DB_ID")) == 0 {
			panic("database ID is required when using a token")
		}
	}

	cluster, err = gocqlastra.NewClusterFromURL("https://api.astra.datastax.com", os.Getenv("ASTRA_DB_ID"), os.Getenv("ASTRA_APPLICATION_TOKEN"), 10*time.Second)

	if err != nil {
		log.Fatalf("unable to load cluster %s from astra: %v", os.Getenv("ASTRA_APPLICATION_TOKEN"), err)
	}

	cluster.Timeout = 30 * time.Second
	session, err := gocql.NewSession(*cluster)

	if err != nil {
		log.Fatalf("unable to connect session: %v", err)
	}

	return &Client{Session: session}, nil
}
