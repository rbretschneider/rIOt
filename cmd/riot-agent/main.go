package main

import (
	"flag"
	"log"

	"github.com/DesyncTheThird/rIOt/internal/agent"
)

var version = "dev"

func main() {
	configPath := flag.String("config", agent.ConfigPath(), "path to config file")
	flag.Parse()

	// Subcommand: riot-agent doctor
	for _, arg := range flag.Args() {
		if arg == "doctor" {
			agent.Doctor(*configPath)
			return
		}
	}

	log.Printf("rIOt agent %s starting", version)

	a, err := agent.New(*configPath, version)
	if err != nil {
		log.Fatalf("failed to create agent: %v", err)
	}

	if err := a.Run(); err != nil {
		log.Fatalf("agent error: %v", err)
	}
}
