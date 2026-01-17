package main

import (
	"github.com/joho/godotenv"
)

func main() {
	godotenv.Overload("cmd/worker/.env")
}
