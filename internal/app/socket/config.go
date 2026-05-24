package socket

import "time"

const (
	// Frontend reconnect backoff caps at 30s, and after a rejected
	// handshake it may take 1+ backoff cycles before a new /getaway
	// call lands. With a 60-second TTL the next /getaway happens at
	// ~T+30 and the handshake at T+30-60s — close enough to the edge
	// that a fresh token has frequently been observed as "expired" by
	// the realtime server. 10 minutes leaves comfortable headroom and
	// is still short enough that a stolen token is low-impact (the
	// frontend re-fetches per connect anyway).
	SocketTTL = 10 * time.Minute
)
