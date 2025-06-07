package main

import (
	"flag"
	"log"
)

func main() {
	var (
		port    = flag.String("port", "8080", "Port to listen on")
		config  = flag.String("config", "config.json", "Configuration file path")
		verbose = flag.Bool("verbose", false, "Enable verbose logging")
	)
	flag.Parse()

	if *verbose {
		log.SetFlags(log.LstdFlags | log.Lshortfile)
	}

	cfg, err := LoadConfig(*config)
	if err != nil {
		log.Printf("Failed to load config from %s, using defaults: %v", *config, err)
		cfg = DefaultConfig()
	}

	proxy := NewProxy(cfg, *verbose)
	
	log.Printf("Starting agentapi-proxy on port %s", *port)
	if err := proxy.GetEcho().Start(":" + *port); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}