package gb28181

type Filter interface {
	AddSource(ssrc uint32, source GBSource) bool

	RemoveSource(ssrc uint32)

	FindSource(ssrc uint32) GBSource
}
