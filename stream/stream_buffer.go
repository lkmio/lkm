package stream

// StreamBuffer GOP缓存
type StreamBuffer interface {

	// AddPacket Return bool 缓存帧是否成功, 如果首帧非关键帧, 缓存失败
	AddPacket(packet interface{}, key bool, ts int64) bool

	// SetDiscardHandler 设置丢弃帧时的回调
	SetDiscardHandler(handler func(packet interface{}))

	Peek(handler func(packet interface{}))

	Duration() int64
}

type streamBuffer struct {
	buffer   RingBuffer
	duration int64

	keyFrameDts         int64 //最近一个关键帧的Dts
	FarthestKeyFrameDts int64 //最远一个关键帧的Dts

	discardHandler func(packet interface{})
}

type element struct {
	ts  int64
	key bool
	pkt interface{}
}

func NewStreamBuffer(duration int64) StreamBuffer {
	return &streamBuffer{duration: duration, buffer: NewRingBuffer(1000)}
}

func (s *streamBuffer) AddPacket(packet interface{}, key bool, ts int64) bool {
	if s.buffer.IsEmpty() {
		if !key {
			return false
		}

		s.FarthestKeyFrameDts = ts
	}

	s.buffer.Push(element{ts, key, packet})
	if key {
		s.keyFrameDts = ts
	}

	//丢弃处理
	//以最近的关键帧时间戳开始，丢弃缓存超过duration长度的帧
	//至少需要保障当前GOP完整
	//暂时不考虑以下情况:
	//	1. 音频收流正常，视频长时间没收流，待视频恢复后。 会造成在此期间，多余的音频帧被丢弃，播放时有画面，没声音.
	//  2. 视频反之亦然
	if !key {
		return true
	}

	for farthest := s.keyFrameDts - s.duration; s.buffer.Size() > 1 && s.buffer.Head().(element).ts < farthest; {
		ele := s.buffer.Pop().(element)

		//重新设置最早的关键帧时间戳
		if ele.key && ele.ts != s.FarthestKeyFrameDts {
			s.FarthestKeyFrameDts = ele.ts
		}

		if s.discardHandler != nil {
			s.discardHandler(ele.pkt)
		}
	}

	return true
}

func (s *streamBuffer) SetDiscardHandler(handler func(packet interface{})) {
	s.discardHandler = handler
}

func (s *streamBuffer) Peek(handler func(packet interface{})) {
	head, tail := s.buffer.All()

	if head == nil {
		return
	}
	for _, value := range head {
		handler(value.(element).pkt)
	}

	if tail == nil {
		return
	}
	for _, value := range tail {
		handler(value.(element).pkt)
	}
}

func (s *streamBuffer) Duration() int64 {
	head := s.buffer.Head()
	tail := s.buffer.Tail()

	if head == nil || tail == nil {
		return 0
	}

	return tail.(element).ts - head.(element).ts
}
