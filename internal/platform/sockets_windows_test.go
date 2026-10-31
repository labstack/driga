package platform

import (
	"context"
	"net"
	"os"
	"testing"

	"github.com/labstack/driga/internal/model"
)

func TestWindowsSocketOwnerTables(t *testing.T) {
	for _, network := range []string{"tcp4", "tcp6", "udp4", "udp6"} {
		t.Run(network, func(t *testing.T) {
			address := "127.0.0.1:0"
			if network[len(network)-1] == '6' {
				address = "[::1]:0"
			}
			if network[:3] == "tcp" {
				ln, err := net.Listen(network, address)
				if err != nil {
					t.Fatal(err)
				}
				defer ln.Close()
			} else {
				ln, err := net.ListenPacket(network, address)
				if err != nil {
					t.Fatal(err)
				}
				defer ln.Close()
			}
			pid := int32(os.Getpid())
			peers, err := New().(SocketProber).ExternalPeers(context.Background(), []int32{pid})
			if err != nil || peers[pid] == model.PeerNone || len(peers) != 1 {
				t.Fatalf("listener ownership: %v %v", peers, err)
			}
		})
	}
}
