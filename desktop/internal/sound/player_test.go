// 文件作用说明：验证本机提示音音量缩放和非法WAV边界，不在测试中播放声音。
package sound

import (
	"encoding/binary"
	"testing"
)

// TestScalePCM 验证正负PCM样本都按成员音量缩放，损坏资源被拒绝。
func TestScalePCM(t *testing.T) {
	raw := make([]byte, 48)
	copy(raw, "RIFF")
	copy(raw[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(raw[16:], 16)
	binary.LittleEndian.PutUint16(raw[20:], 1)
	binary.LittleEndian.PutUint16(raw[34:], 16)
	copy(raw[36:], "data")
	binary.LittleEndian.PutUint32(raw[40:], 4)
	binary.LittleEndian.PutUint16(raw[44:], 1000)
	negative := int16(-1000)
	binary.LittleEndian.PutUint16(raw[46:], uint16(negative))
	if err := scalePCM(raw, .5); err != nil {
		t.Fatal(err)
	}
	if int16(binary.LittleEndian.Uint16(raw[44:])) != 500 || int16(binary.LittleEndian.Uint16(raw[46:])) != -500 {
		t.Fatal("提示音音量缩放错误")
	}
	if scalePCM([]byte("invalid"), .5) == nil {
		t.Fatal("未拒绝损坏提示音")
	}
}
