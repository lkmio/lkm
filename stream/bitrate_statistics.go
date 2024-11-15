package stream

import "time"

// BitrateStatistics 码流统计, 单位Byte
type BitrateStatistics struct {
	totalBytes     int64 // 总共传输的字节数
	elapsedSeconds int   // 经过的秒数
	currentSecond  int   // 当前秒数

	previousSecondBytes int // 前一秒传输的字节数
	latestSecondBytes   int // 当前秒正在传输的字节数
}

func (b *BitrateStatistics) Input(size int) {
	b.totalBytes += int64(size)

	second := time.Now().Second()
	if b.currentSecond == -1 {
		b.currentSecond = second
	}

	if second != b.currentSecond {
		b.elapsedSeconds++
		b.currentSecond = second
		b.previousSecondBytes = b.latestSecondBytes
		b.latestSecondBytes = 0
	}

	b.latestSecondBytes += size
}

// Average 返回每秒平均码流大小
func (b *BitrateStatistics) Average() int {
	if b.elapsedSeconds < 1 {
		return b.latestSecondBytes
	}

	return int((b.totalBytes - int64(b.latestSecondBytes)) / int64(b.elapsedSeconds))
}

// Total 返回总码流大小
func (b *BitrateStatistics) Total() int64 {
	return b.totalBytes
}

// PreviousSecond 返回前一秒的码流大小
func (b *BitrateStatistics) PreviousSecond() int {
	return b.previousSecondBytes
}

func NewBitrateStatistics() *BitrateStatistics {
	return &BitrateStatistics{
		currentSecond: -1,
	}
}
