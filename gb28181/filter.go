package gb28181

// Filter 关联Source
type Filter interface {
	AddSource(ssrc uint32, source GBSource) bool

	RemoveSource(ssrc uint32)

	FindSource(ssrc uint32) GBSource
}
