package store

import (
	"errors"
	"testing"
)

func TestRemoteScopeLANHTTPAndRelayHTTPS(t *testing.T) {
	for _, scope := range []RemoteScope{
		{Mode: "lan", Origin: "http://192.168.1.10:18443"},
		{Mode: "lan", Origin: "http://127.0.0.1:18443"},
		{Mode: "lan", Origin: "http://192.168.1.10"},
		{Mode: "relay", Origin: "https://phone.example.com"},
	} {
		t.Run(scope.Mode+"_"+scope.Origin, func(t *testing.T) {
			if err := scope.Validate(); err != nil {
				t.Fatalf("expected supported origin, got %v", err)
			}
		})
	}
	for _, scope := range []RemoteScope{
		{Mode: "lan", Origin: "https://192.168.1.10:18443"},
		{Mode: "lan", Origin: "http://localhost:18443"},
		{Mode: "lan", Origin: "http://[::1]:18443"},
		{Mode: "lan", Origin: "http://[::ffff:127.0.0.1]:18443"},
		{Mode: "lan", Origin: "http://192.168.1.999:18443"},
		{Mode: "lan", Origin: "http://fixture-user:fixture-password@192.168.1.10:18443"},
		{Mode: "lan", Origin: "http://192.168.1.10:18443/"},
		{Mode: "lan", Origin: "http://192.168.1.10:18443/path"},
		{Mode: "lan", Origin: "http://192.168.1.10:18443?secret=yes"},
		{Mode: "lan", Origin: "http://192.168.1.10:18443?"},
		{Mode: "lan", Origin: "http://192.168.1.10:18443#fragment"},
		{Mode: "relay", Origin: "http://phone.example.com"},
		{Mode: "relay", Origin: "https://fixture-user:fixture-password@phone.example.com"},
		{Mode: "relay", Origin: "https://phone.example.com/path"},
		{Mode: "relay", Origin: "https://phone.example.com?secret=yes"},
		{Mode: "relay", Origin: "https://phone.example.com#fragment"},
	} {
		t.Run(scope.Mode+"_"+scope.Origin, func(t *testing.T) {
			if err := scope.Validate(); !errors.Is(err, ErrInvalidRemote) {
				t.Fatalf("expected invalid scope, got %v", err)
			}
		})
	}
}
