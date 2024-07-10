package stream

import (
	"fmt"
	"github.com/lkmio/avformat/utils"
)

type TransStreamFactory func(source Source, protocol Protocol, streams []utils.AVStream) (TransStream, error)

var (
	transStreamFactories map[Protocol]TransStreamFactory
)

func init() {
	transStreamFactories = make(map[Protocol]TransStreamFactory, 8)
}

func RegisterTransStreamFactory(protocol Protocol, streamFunc TransStreamFactory) {
	_, ok := transStreamFactories[protocol]
	if ok {
		panic(fmt.Sprintf("%s has been registered", protocol.ToString()))
	}

	transStreamFactories[protocol] = streamFunc
}

func FindTransStreamFactory(protocol Protocol) (TransStreamFactory, error) {
	f, ok := transStreamFactories[protocol]
	if !ok {
		return nil, fmt.Errorf("unknown protocol %s", protocol.ToString())
	}

	return f, nil
}

func CreateTransStream(source Source, protocol Protocol, streams []utils.AVStream) (TransStream, error) {
	factory, err := FindTransStreamFactory(protocol)
	if err != nil {
		return nil, err
	}

	return factory(source, protocol, streams)
}
