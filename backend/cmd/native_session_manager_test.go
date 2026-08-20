package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNativeSessionManagerLocalURL(t *testing.T) {
	tests := []struct {
		name   string
		listen string
		want   string
	}{
		{name: "all IPv4 interfaces", listen: ":8080", want: "http://127.0.0.1:8080"},
		{name: "explicit all IPv4 interfaces", listen: "0.0.0.0:8081", want: "http://127.0.0.1:8081"},
		{name: "all IPv6 interfaces", listen: "[::]:8082", want: "http://127.0.0.1:8082"},
		{name: "explicit loopback", listen: "127.0.0.1:8083", want: "http://127.0.0.1:8083"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := nativeSessionManagerLocalURL(tt.listen)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestNativeSessionManagerLocalURLRejectsInvalidListenAddress(t *testing.T) {
	_, err := nativeSessionManagerLocalURL("8080")
	require.Error(t, err)
}
