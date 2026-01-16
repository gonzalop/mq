package packets

import "sync"

// bufferPool is a pool of byte slices for reading packets.
// Fixed 4KB size is suitable for most control packets and small messages.
// Larger packets will still allocate.
var bufferPool = sync.Pool{
	New: func() any {
		// 4KB buffer covers most typical MQTT messages
		buf := make([]byte, 4096)
		return &buf
	},
}

// getBuffer returns a buffer from the pool.
// If the requested size is larger than the pooled buffer, it allocates a new one.
func getBuffer(size int) *[]byte {
	if size > 4096 {
		buf := make([]byte, size)
		return &buf
	}
	return bufferPool.Get().(*[]byte)
}

// putBuffer returns a buffer to the pool.
// Only pooled buffers (<= 4096 capacity) should be returned.
func putBuffer(bufPtr *[]byte) {
	if cap(*bufPtr) != 4096 {
		return
	}
	bufferPool.Put(bufPtr)
}
