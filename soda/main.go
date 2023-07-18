package main

import (
	"github.com/WilliamNHarvey/pop/v6/soda/cmd"
)

func main() {
	cmd.RootCmd.Use = "soda"
	cmd.Execute()
}
