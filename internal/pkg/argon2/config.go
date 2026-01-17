package argon2

const (
	saltLength = 16
	keyLength  = 32
)

var (
	memory      = uint32(64 * 1024) // 64 MB
	iterations  = uint32(3)
	parallelism = uint8(2)
)
