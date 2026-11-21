// Command omt-discovery-server runs an OMT discovery server.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	omt "github.com/MikanseiLaboratory/omt-go"
)

func main() {
	bind := flag.String("bind", "", "bind address (empty = all interfaces)")
	port := flag.Int("port", omt.DiscoveryServerPort, "TCP port")
	flag.Parse()
	s, err := omt.ListenDiscoveryServer(*bind, *port)
	if err != nil {
		fmt.Fprintln(os.Stderr, "listen:", err)
		os.Exit(1)
	}
	defer s.Close()
	fmt.Printf("OMT discovery server listening on %s\n", s.Addr())
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	<-ch
}
